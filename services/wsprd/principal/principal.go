// Package principal implements a principal manager that maps origins to
// vanadium principals.
//
// Each instance of wspr is expected to have a single (human) user at a time
// that will be signed in and using various apps. A user may have different
// Blessings, which in practice will be done by having multiple accounts
// across many Blessing providers (e.g., google, facebook, etc). This is similar
// to having a master identity that is linked to multiple identities in today's
// technology. In our case, each user account is represented as a Blessing
// obtained from the corresponding Blessing provider.
//
// Every app/origin is a different principal and has its own public/private key
// pair, represented by a Principal object that is created by this manager on
// the app's behalf. For each app/origin, the user may choose which account to
// use for the app, which results in a blessing generated for the app principal
// using the selected account's blessing.
//
// For example, a user Alice may have an account at Google and Facebook,
// resulting in the blessings "google/alice@gmail.com" and
// "facebook/alice@facebook.com". She may then choose to use her Google account
// for the "googleplay" app, which would result in the creation of a new
// principal for that app and a blessing "google/alice@gmail.com/googleplay" for
// that principal.
//
// The principal manager only serializes the mapping from apps to (chosen)
// accounts and the account information, but not the private keys for each app.
package principal

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"sync"
	"time"

	vsecurity "v.io/x/ref/security"
	"v.io/x/ref/security/serialization"

	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vom"
)

// permissions is a set of a permissions given to an app, containing the account
// the app has access to and the caveats associated with it.
type permissions struct {
	// Account is the name of the account given to the app.
	Account string
	// Caveats that must be added to the blessing generated for the app from
	// the account's blessing.
	Caveats []security.Caveat
	// Expirations is the expiration times of any expiration caveats.  Must
	// be unix-time so it can be persisted.
	Expirations []int64
}

// persistentState is the state of the manager that will be persisted to disk.
type persistentState struct {
	// A mapping of origins to the permissions provided for the origin (such as
	// caveats and the account given to the origin)
	Origins map[string]permissions

	// A set of accounts that maps from an account name to the Blessings associated
	// with the account.
	Accounts map[string]security.Blessings
}

const pkgPath = "v.io/x/ref/services/wsprd/principal"

// Errors.
var (
	errUnknownAccount                   = verror.Register(pkgPath+".errUnknownAccount", verror.NoRetry, "{1:}{2:} unknown account{:_}")
	errFailedToCreatePrincipal          = verror.Register(pkgPath+".errFailedToCreatePrincipal", verror.NoRetry, "{1:}{2:} failed to create new principal{:_}")
	errFailedToConstructBlessings       = verror.Register(pkgPath+".errFailedToConstructBlessings", verror.NoRetry, "{1:}{2:} failed to construct Blessings to bless with{:_}")
	errFailedToBlessPrincipal           = verror.Register(pkgPath+".errFailedToBlessPrincipal", verror.NoRetry, "{1:}{2:} failed to bless new principal with the provided account{:_}")
	errFailedToSetDefaultBlessings      = verror.Register(pkgPath+".errFailedToSetDefaultBlessings", verror.NoRetry, "{1:}{2:} failed to set account blessings as default{:_}")
	errFailedToSetAllPrincipalBlessings = verror.Register(pkgPath+".errFailedToSetAllPrincipalBlessings", verror.NoRetry, "{1:}{2:} failed to set account blessings for all principals{:_}")
	errFailedToAddRoots                 = verror.Register(pkgPath+".errFailedToAddRoots", verror.NoRetry, "{1:}{2:} failed to add roots of account blessing{:_}")
)

// bufferCloser implements io.ReadWriteCloser.
type bufferCloser struct {
	bytes.Buffer
}

func (*bufferCloser) Close() error {
	return nil
}

// InMemorySerializer implements SerializerReaderWriter. This Serializer should only
// be used in tests.
type InMemorySerializer struct {
	data      bufferCloser
	signature bufferCloser
	hasData   bool
}

func (s *InMemorySerializer) Readers() (io.ReadCloser, io.ReadCloser, error) {
	if !s.hasData {
		return nil, nil, nil
	}
	return &s.data, &s.signature, nil
}

func (s *InMemorySerializer) Writers() (io.WriteCloser, io.WriteCloser, error) {
	s.hasData = true
	s.data.Reset()
	s.signature.Reset()
	return &s.data, &s.signature, nil
}

// PrincipalManager manages app principals. We only serialize the accounts
// associated with this principal manager and the mapping of apps to permissions
// that they were given.
type PrincipalManager struct {
	mu    sync.Mutex
	state persistentState

	// root is the Principal that hosts this PrincipalManager.
	root security.Principal

	serializer vsecurity.SerializerReaderWriter

	// Dummy account name
	// TODO(bjornick, nlacasse): Remove this once the tests no longer need
	// it.
	dummyAccount string
}

// NewPrincipalManager returns a new PrincipalManager that creates new principals
// for various app/origins and blessed them with blessings for the provided 'root'
// principal from the specified blessing provider.
// .
//
// It is initialized by reading data from the 'serializer' passed in which must
// be non-nil.
func NewPrincipalManager(root security.Principal, serializer vsecurity.SerializerReaderWriter) (*PrincipalManager, error) {
	result := &PrincipalManager{
		state: persistentState{
			Origins:  map[string]permissions{},
			Accounts: map[string]security.Blessings{},
		},
		root:       root,
		serializer: serializer,
	}
	data, signature, err := serializer.Readers()
	if err != nil {
		return nil, err
	}
	if (data == nil) || (signature == nil) {
		// No serialized data exists, returning an empty PrincipalManager.
		return result, nil
	}
	vr, err := serialization.NewVerifyingReader(data, signature, root.PublicKey())
	if err != nil {
		return nil, err
	}

	decoder, err := vom.NewDecoder(vr)

	if err != nil {
		return nil, err
	}

	if err := decoder.Decode(&result.state); err != nil {
		return nil, err
	}
	return result, nil
}

func (i *PrincipalManager) save() error {
	data, signature, err := i.serializer.Writers()
	if err != nil {
		return err
	}
	swc, err := serialization.NewSigningWriteCloser(data, signature, i.root, nil)
	if err != nil {
		return err
	}

	encoder, err := vom.NewEncoder(swc)

	if err != nil {
		return err
	}
	if err := encoder.Encode(i.state); err != nil {
		return err
	}
	return swc.Close()
}

// Principal returns the Principal for an origin or an error if there is
// no linked account.
func (i *PrincipalManager) Principal(origin string) (security.Principal, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	perm, found := i.state.Origins[origin]
	if !found {
		return nil, verror.New(verror.ErrNoExist, nil, origin)
	}
	blessings, found := i.state.Accounts[perm.Account]
	if !found {
		return nil, verror.New(errUnknownAccount, nil, perm.Account)
	}
	return i.createPrincipal(origin, blessings, perm.Caveats)
}

// OriginHasAccount returns true iff the origin has been associated with
// permissions and an account for which blessings have been obtained from a
// blessing provider, and if the blessings have no expiration caveats or an
// expiration in the future.
func (i *PrincipalManager) OriginHasAccount(origin string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	perm, found := i.state.Origins[origin]
	if !found {
		return false
	}

	// Check if all expiration caveats are satisfied.
	now := time.Now()
	for _, unixExp := range perm.Expirations {
		exp := time.Unix(unixExp, 0)
		if exp.Before(now) {
			return false
		}
	}

	_, found = i.state.Accounts[perm.Account]
	return found
}

// BlessingsForAccount returns the Blessing associated with the provided
// account.  It returns an error if account does not exist.
//
// TODO(ataly, ashankar, bjornick): Modify this method to allow searching
// for accounts from a specific root blessing provider. One option is
// that the method could take a set of root blessing providers as argument
// and then return accounts whose blessings are from one of these providers.
func (i *PrincipalManager) BlessingsForAccount(account string) (security.Blessings, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	blessings, found := i.state.Accounts[account]
	if !found {
		return security.Blessings{}, verror.New(errUnknownAccount, nil, account)
	}
	return blessings, nil
}

// AddAccount associates the provided Blessing with the provided account.
func (i *PrincipalManager) AddAccount(account string, blessings security.Blessings) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	old, existed := i.state.Accounts[account]
	i.state.Accounts[account] = blessings

	if err := i.save(); err != nil {
		delete(i.state.Accounts, account)
		if existed {
			i.state.Accounts[account] = old
		}
		return err
	}
	return nil
}

// AddOrigin adds an origin to the manager linked to the given account.
func (i *PrincipalManager) AddOrigin(origin string, account string, caveats []security.Caveat, expirations []time.Time) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, found := i.state.Accounts[account]; !found {
		return verror.New(errUnknownAccount, nil, account)
	}

	unixExpirations := []int64{}
	for _, exp := range expirations {
		unixExpirations = append(unixExpirations, exp.Unix())
	}

	old, existed := i.state.Origins[origin]
	i.state.Origins[origin] = permissions{account, caveats, unixExpirations}

	if err := i.save(); err != nil {
		delete(i.state.Origins, origin)
		if existed {
			i.state.Origins[origin] = old
		}
		return err
	}
	return nil
}

func (i *PrincipalManager) createPrincipal(origin string, withBlessings security.Blessings, caveats []security.Caveat) (security.Principal, error) {
	ret, err := vsecurity.NewPrincipal()
	if err != nil {
		return nil, verror.New(errFailedToCreatePrincipal, nil, err)
	}

	if len(caveats) == 0 {
		caveats = append(caveats, security.UnconstrainedUse())
	}
	// Origins have the form protocol://hostname:port, which is not a valid
	// blessing extension. Hence we must url-encode.
	blessings, err := i.root.Bless(ret.PublicKey(), withBlessings, url.QueryEscape(origin), caveats[0], caveats[1:]...)
	if err != nil {
		return nil, verror.New(errFailedToBlessPrincipal, nil, err)
	}

	if err := ret.BlessingStore().SetDefault(blessings); err != nil {
		return nil, verror.New(errFailedToSetDefaultBlessings, nil, err)
	}
	if _, err := ret.BlessingStore().Set(blessings, security.AllPrincipals); err != nil {
		return nil, verror.New(errFailedToSetAllPrincipalBlessings, nil, err)
	}
	if err := ret.AddToRoots(blessings); err != nil {
		return nil, verror.New(errFailedToAddRoots, nil, err)
	}
	return ret, nil
}

// Add dummy account with default blessings, for use by unauthenticated
// clients.
// TODO(nlacasse, bjornick): This should go away once unauthenticate clients
// are no longer allowed.
func (i *PrincipalManager) DummyAccount() (string, error) {
	if i.dummyAccount == "" {
		// Note: We only set i.dummyAccount once the account has been
		// successfully created.  Otherwise, if an error occurs, the
		// next time this function is called it the account won't exist
		// but this function will return the name of the account
		// without trying to create it.
		dummyAccount := "unauthenticated-dummy-account"
		blessings, err := i.root.BlessSelf(dummyAccount)
		if err != nil {
			return "", fmt.Errorf("i.root.BlessSelf(%v) failed: %v", dummyAccount, err)
		}

		if err := i.AddAccount(dummyAccount, blessings); err != nil {
			return "", fmt.Errorf("browspr.principalManager.AddAccount(%v, %v) failed: %v", dummyAccount, blessings, err)
		}
		i.dummyAccount = dummyAccount
	}
	return i.dummyAccount, nil
}

// Package principal implements a principal manager that maps origins to
// veyron principals.
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
	"io"
	"net/url"
	"sync"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/serialization"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vom"
)

// permissions is a set of a permissions given to an app, containing the account
// the app has access to and the caveats associated with it.
type permissions struct {
	// Account is the name of the account given to the app.
	Account string
	// Caveats that must be added to the blessing generated for the app from
	// the account's blessing.
	Caveats []security.Caveat
}

// persistentState is the state of the manager that will be persisted to disk.
type persistentState struct {
	// A mapping of origins to the permissions provided for the origin (such as
	// caveats and the account given to the origin)
	Origins map[string]permissions

	// A set of accounts that maps from an account name to the Blessings associated
	// with the account.
	Accounts map[string]security.WireBlessings
}

// Serializer is a factory for managing the readers and writers used by the
// PrincipalManager for serialization and deserialization
type Serializer interface {
	// Readers returns io.Readers for reading the PrincipalManager's serialized
	// data and its signature.
	Readers() (data io.Reader, signature io.Reader, err error)
	// Writers returns io.WriteClosers for writing the PrincipalManager's
	// serialized data and integrity signature.
	Writers() (data io.WriteCloser, signature io.WriteCloser, err error)
}

var OriginDoesNotExist = verror.NoExistf("origin not found")

// PrincipalManager manages app principals. We only serialize the accounts
// associated with this principal manager and the mapping of apps to permissions
// that they were given.
type PrincipalManager struct {
	mu    sync.Mutex
	state persistentState

	// root is the Principal that hosts this PrincipalManager.
	root security.Principal

	serializer Serializer
}

// NewPrincipalManager returns a new PrincipalManager that creates new principals
// for various app/origins and blessed them with blessings for the provided 'root'
// principal from the specified blessing provider.
// .
//
// It is initialized by reading data from the 'serializer' passed in which must
// be non-nil.
func NewPrincipalManager(root security.Principal, serializer Serializer) (*PrincipalManager, error) {
	result := &PrincipalManager{
		state: persistentState{
			Origins:  map[string]permissions{},
			Accounts: map[string]security.WireBlessings{},
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
	if err := vom.NewDecoder(vr).Decode(&result.state); err != nil {
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
	if err := vom.NewEncoder(swc).Encode(i.state); err != nil {
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
		return nil, verror.NoExistf("origin not found")
	}
	wireBlessings, found := i.state.Accounts[perm.Account]
	if !found {
		return nil, verror.NoExistf("unknown account %s", perm.Account)
	}
	return i.createPrincipal(origin, wireBlessings, perm.Caveats)
}

// BlessingsForAccount returns the Blessing associated with the provided
// account.  It returns an error if account does not exist.
func (i *PrincipalManager) BlessingsForAccount(account string) (security.Blessings, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	wireBlessings, found := i.state.Accounts[account]
	if !found {
		return nil, verror.NoExistf("unknown account %s", account)
	}
	return security.NewBlessings(wireBlessings)
}

// AddAccount associates the provided Blessing with the provided account.
func (i *PrincipalManager) AddAccount(account string, blessings security.Blessings) error {
	wireBlessings := security.MarshalBlessings(blessings)
	i.mu.Lock()
	defer i.mu.Unlock()

	old, existed := i.state.Accounts[account]
	i.state.Accounts[account] = wireBlessings

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
func (i *PrincipalManager) AddOrigin(origin string, account string, caveats []security.Caveat) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, found := i.state.Accounts[account]; !found {
		return verror.NoExistf("unknown account %s", account)
	}

	old, existed := i.state.Origins[origin]
	i.state.Origins[origin] = permissions{account, caveats}

	if err := i.save(); err != nil {
		delete(i.state.Origins, origin)
		if existed {
			i.state.Origins[origin] = old
		}
		return err
	}
	return nil
}

func (i *PrincipalManager) createPrincipal(origin string, wireBlessings security.WireBlessings, caveats []security.Caveat) (security.Principal, error) {
	ret, err := vsecurity.NewPrincipal()
	if err != nil {
		return nil, verror.Internalf("failed to create new principal: %v", err)
	}

	withBlessings, err := security.NewBlessings(wireBlessings)
	if err != nil {
		return nil, verror.Internalf("failed to construct Blessings to bless with: %v", err)
	}

	if len(caveats) == 0 {
		caveats = append(caveats, security.UnconstrainedUse())
	}
	// Origins have the form protocol://hostname:port, which is not a valid
	// blessing extension. Hence we must url-encode.
	blessings, err := i.root.Bless(ret.PublicKey(), withBlessings, url.QueryEscape(origin), caveats[0], caveats[1:]...)
	if err != nil {
		return nil, verror.NoAccessf("failed to bless new principal with the provided account: %v", err)
	}

	if err := ret.BlessingStore().SetDefault(blessings); err != nil {
		return nil, verror.Internalf("failed to set account blessings as default: %v", err)
	}
	if _, err := ret.BlessingStore().Set(blessings, security.AllPrincipals); err != nil {
		return nil, verror.Internalf("failed to set account blessings for all principals: %v", err)
	}
	if err := ret.AddToRoots(blessings); err != nil {
		return nil, verror.Internalf("failed to add roots of account blessing: %v", err)
	}
	return ret, nil
}

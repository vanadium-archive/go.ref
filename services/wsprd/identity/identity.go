// Implements an identity manager that maps origins to veyron identities.  Each instance
// of wspr is expected to have only one user at a time that will be signed in.  In this case,
// the user means the person using the app.  Each user may use many different names, which in
// practice will be done by having multiple accounts across many identity providers (i.e google,
// facebook,etc).  This is similar to having a master identity that is linked to multiple identities
// in today's technology. For each app/origin, the user may choose which name to provide that app,
// which results in a blessed identity for that app.  Each blessed identity will have a different
// private key and ideally, all accounts will share the same private key, but for now they are also
// separate.  The identity manager only serializes the mapping of app to account and the account
// information, but not the private keys for each app.
// TODO(bjornick,ataly,ashankar): Have all the accounts share the same private key which will be stored
// in a TPM, so no private key gets serialized to disk.
package identity

import (
	"crypto/sha256"
	"io"
	"net/url"
	"sync"
	"time"

	"veyron2"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vom"
)

// permissions is a set of a permissions given to an app, containing the account
// the app has access to and the caveats associated with it.
type permissions struct {
	// The account name that is given to an app.
	Account string
	Caveats []security.ServiceCaveat
}

// persistentState is the state of the manager that will be persisted to disk.
type persistentState struct {
	// A mapping of origins to the permissions provide for the origin (such as
	// caveats and the account given to the origin)
	Origins map[string]permissions

	// A set of accounts that maps from a name to the account.
	Accounts map[string]security.PrivateID
}

// Serializer is a factory for managing the readers and writers used by the IDManager
// for serialization and deserialization
type Serializer interface {
	// DataWriter returns a writer that is used to write the data portion
	// of the IDManager
	DataWriter() io.WriteCloser

	// SignatureWriter returns a writer that is used to write the signature
	// of the serialized data.
	SignatureWriter() io.WriteCloser

	// DataReader returns a reader that is used to read the serialized data.
	// If nil is returned, then there is no seralized data to load.
	DataReader() io.Reader

	// SignatureReader returns a reader that is used to read the signature of the
	// serialized data.  If nil is returned, then there is no signature to load.
	SignatureReader() io.Reader
}

var OriginDoesNotExist = verror.NotFoundf("origin not found")

// IDManager manages app identities.  We only serialize the accounts associated with
// this id manager and the mapping of apps to permissions that they were given.
type IDManager struct {
	mu    sync.Mutex
	state persistentState

	// The runtime that will be used to create new identities
	rt veyron2.Runtime

	serializer Serializer
}

// NewIDManager creates a new IDManager from the reader passed in. serializer can't be nil
func NewIDManager(rt veyron2.Runtime, serializer Serializer) (*IDManager, error) {
	result := &IDManager{
		rt: rt,
		state: persistentState{
			Origins:  map[string]permissions{},
			Accounts: map[string]security.PrivateID{},
		},
		serializer: serializer,
	}

	reader := serializer.DataReader()
	var hadData bool
	hash := sha256.New()
	if reader != nil {
		hadData = true
		if err := vom.NewDecoder(io.TeeReader(reader, hash)).Decode(&result.state); err != nil {
			return nil, err
		}

	}
	signed := hash.Sum(nil)

	var sig security.Signature

	reader = serializer.SignatureReader()

	var hadSig bool
	if reader != nil {
		hadSig = true
		if err := vom.NewDecoder(serializer.SignatureReader()).Decode(&sig); err != nil {
			return nil, err
		}
	}

	if !hadSig && !hadData {
		return result, nil
	}

	if !sig.Verify(rt.Identity().PublicID().PublicKey(), signed) {
		return nil, verror.NotAuthorizedf("signature verification failed")
	}

	return result, nil
}

// Save serializes the IDManager to the writer.
func (i *IDManager) save() error {
	hash := sha256.New()
	writer := i.serializer.DataWriter()

	if err := vom.NewEncoder(io.MultiWriter(writer, hash)).Encode(i.state); err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	signed := hash.Sum(nil)
	signature, err := i.rt.Identity().Sign(signed)

	if err != nil {
		return err
	}

	writer = i.serializer.SignatureWriter()

	if err := vom.NewEncoder(writer).Encode(signature); err != nil {
		return err
	}

	return writer.Close()
}

// Identity returns the identity for an origin.  Returns OriginDoesNotExist if
// there is no identity for the origin.
func (i *IDManager) Identity(origin string) (security.PrivateID, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	perm, found := i.state.Origins[origin]
	if !found {
		return nil, OriginDoesNotExist
	}
	blessedID, err := i.generateBlessedID(origin, perm.Account, perm.Caveats)
	if err != nil {
		return nil, err
	}
	return blessedID, nil
}

// AccountsMatching returns a list of accounts that match the given pattern.
func (i *IDManager) AccountsMatching(trustedRoot security.PrincipalPattern) []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	result := []string{}
	for name, id := range i.state.Accounts {
		if id.PublicID().Match(trustedRoot) {
			result = append(result, name)
		}
	}
	return result
}

// AddAccount associates a PrivateID with an account name.
func (i *IDManager) AddAccount(name string, id security.PrivateID) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	old, existed := i.state.Accounts[name]
	i.state.Accounts[name] = id
	if err := i.save(); err != nil {
		delete(i.state.Accounts, name)
		if existed {
			i.state.Accounts[name] = old
		}
		return err
	}
	return nil
}

// AddOrigin adds an origin to the manager linked to a the given account.
func (i *IDManager) AddOrigin(origin string, account string, caveats []security.ServiceCaveat) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, found := i.state.Accounts[account]; !found {
		return verror.NotFoundf("unknown account %s", account)
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

func (i *IDManager) generateBlessedID(origin string, account string, caveats []security.ServiceCaveat) (security.PrivateID, error) {
	blessor := i.state.Accounts[account]
	if blessor == nil {
		return nil, verror.NotFoundf("unknown account %s", account)
	}
	// Origins have the form protocol://hostname:port, which is not a valid
	// blessing name. Hence we must url-encode.
	name := url.QueryEscape(origin)
	blessee, err := i.rt.NewIdentity(name)
	if err != nil {
		return nil, err
	}

	blessed, err := blessor.Bless(blessee.PublicID(), name, 24*time.Hour, caveats)

	if err != nil {
		return nil, verror.NotAuthorizedf("failed to bless id: %v", err)
	}

	if blessee, err = blessee.Derive(blessed); err != nil {
		return nil, verror.Internalf("failed to derive private id: %v", err)
	}
	return blessee, nil
}

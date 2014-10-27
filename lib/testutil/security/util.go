// Package security contains utility testing functions related to
// security.
package security

import (
	"io/ioutil"
	"os"

	vsecurity "veyron.io/veyron/veyron/security"

	"veyron.io/veyron/veyron2/security"
)

// NewVeyronCredentials generates a directory with a new principal
// that can be used as a value for the VEYRON_CREDENTIALS environment
// variable to initialize a Runtime.
//
// The principal created uses a blessing from 'parent', with the extension
// 'name' as its default blessing.
//
// It returns the path to the directory created.
func NewVeyronCredentials(parent security.Principal, name string) string {
	dir, err := ioutil.TempDir("", "veyron_credentials")
	if err != nil {
		panic(err)
	}
	p, err := vsecurity.LoadPersistentPrincipal(dir, nil)
	if err != nil {
		if p, err = vsecurity.CreatePersistentPrincipal(dir, nil); err != nil {
			panic(err)
		}
	}
	blessings, err := parent.Bless(p.PublicKey(), parent.BlessingStore().Default(), name, security.UnconstrainedUse())
	if err != nil {
		panic(err)
	}
	SetDefaultBlessings(p, blessings)
	return dir
}

// SetDefaultBlessings updates the BlessingStore and BlessingRoots of p
// so that:
// (1) b is revealed to all clients that connect to Servers operated
// by 'p' (BlessingStore.Default)
// (2) b is revealed  to all servers that clients connect to on behalf
// of p (BlessingStore.Set(..., security.AllPrincipals))
// (3) p recognizes all blessings that have the same root certificate as b.
// (AddToRoots)
func SetDefaultBlessings(p security.Principal, b security.Blessings) {
	if err := p.BlessingStore().SetDefault(b); err != nil {
		panic(err)
	}
	if _, err := p.BlessingStore().Set(b, security.AllPrincipals); err != nil {
		panic(err)
	}
	if err := p.AddToRoots(b); err != nil {
		panic(err)
	}
}

// SaveACLToFile saves the provided ACL in JSON format to a randomly created
// temporary file, and returns the path to the file. This function is meant
// to be used for testing purposes only, it panics if there is an error. The
// caller must ensure that the created file is removed once it is no longer needed.
func SaveACLToFile(acl security.ACL) string {
	f, err := ioutil.TempFile("", "saved_acl")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := vsecurity.SaveACL(f, acl); err != nil {
		defer os.Remove(f.Name())
		panic(err)
	}
	return f.Name()
}

// IDProvider is a convenience wrapper over security.Principal that
// makes a Principal act as an "identity provider" (i.e., provides
// other principals with a blessing from it).
type IDProvider struct {
	p security.Principal
	b security.Blessings
}

func NewIDProvider(name string) *IDProvider {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	b, err := p.BlessSelf(name)
	if err != nil {
		panic(err)
	}
	return &IDProvider{p, b}
}

// Bless sets up the provided principal to use blessings from idp as its
// default.
func (idp *IDProvider) Bless(who security.Principal, extension string, caveats ...security.Caveat) error {
	if len(caveats) == 0 {
		caveats = append(caveats, security.UnconstrainedUse())
	}
	blessings, err := idp.p.Bless(who.PublicKey(), idp.b, extension, caveats[0], caveats[1:]...)
	if err != nil {
		return err
	}
	SetDefaultBlessings(who, blessings)
	return nil
}

// PublicKey is the public key of the identity provider.
func (idp *IDProvider) PublicKey() security.PublicKey {
	return idp.p.PublicKey()
}

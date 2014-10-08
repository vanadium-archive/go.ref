package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"os"
	"path"

	"veyron.io/veyron/veyron2/security"
)

const privateKeyFile = "privatekey.pem"

// newKey generates an ECDSA (public, private) key pair..
func newKey() (security.PublicKey, *ecdsa.PrivateKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return security.NewECDSAPublicKey(&priv.PublicKey), priv, nil
}

// NewPrincipal mints a new private key and generates a principal based on
// this key, storing its BlessingRoots and BlessingStore in memory.
func NewPrincipal() (security.Principal, error) {
	pub, priv, err := newKey()
	if err != nil {
		return nil, err
	}
	return security.CreatePrincipal(security.NewInMemoryECDSASigner(priv), newInMemoryBlessingStore(pub), newInMemoryBlessingRoots())
}

// NewPersistentPrincipal reads state for a principal (private key, BlessingRoots, BlessingStore)
// from the provided directory 'dir' and commits all state changes to the same directory.
//
// If the directory does not contain state, then a new private key is minted and all state of
// the principal is committed to 'dir'. If the directory does not exist, it is created.
//
// Returns the security.Principal implementation, a boolean that is set to true iff the directory
// already contained a principal and any errors in initialization.
func NewPersistentPrincipal(dir string) (principal security.Principal, existed bool, err error) {
	if finfo, err := os.Stat(dir); err == nil {
		if !finfo.IsDir() {
			return nil, false, fmt.Errorf("%q is not a directory", dir)
		}
	} else if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, false, fmt.Errorf("failed to create %q: %v", dir, err)
	}
	key, existed, err := initKey(dir)
	if err != nil {
		return nil, false, fmt.Errorf("could not initialize private key from credentials directory %v: %v", dir, err)
	}
	signer, err := security.CreatePrincipal(security.NewInMemoryECDSASigner(key), nil, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create serialization.Signer: %v", err)
	}
	roots, err := newPersistingBlessingRoots(dir, signer)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load BlessingRoots from %q: %v", dir, err)
	}
	store, err := newPersistingBlessingStore(dir, signer)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load BlessingStore from %q: %v", dir, err)
	}
	p, err := security.CreatePrincipal(security.NewInMemoryECDSASigner(key), store, roots)
	return p, existed, err
}

func initKey(dir string) (*ecdsa.PrivateKey, bool, error) {
	keyFile := path.Join(dir, privateKeyFile)
	if f, err := os.Open(keyFile); err == nil {
		defer f.Close()
		v, err := LoadPEMKey(f, nil)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load PEM data from %q: %v", keyFile, v)
		}
		key, ok := v.(*ecdsa.PrivateKey)
		if !ok {
			return nil, false, fmt.Errorf("%q contains a %T, not an ECDSA private key", keyFile, v)
		}
		return key, true, nil
	} else if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("failed to read %q: %v", keyFile, err)
	}
	_, key, err := newKey()
	if err != nil {
		return nil, false, fmt.Errorf("failed to generate a private key: %v", err)
	}
	f, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open %q for writing: %v", keyFile, err)
	}
	defer f.Close()
	if err := SavePEMKey(f, key, nil); err != nil {
		return nil, false, fmt.Errorf("failed to save private key to %q: %v", keyFile, err)
	}
	return key, false, nil
}

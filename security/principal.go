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

// NewPrincipal mints a new private key and generates a principal based on
// this key, storing its BlessingRoots and BlessingStore in memory.
func NewPrincipal() (security.Principal, error) {
	pub, priv, err := newKey()
	if err != nil {
		return nil, err
	}
	return security.CreatePrincipal(security.NewInMemoryECDSASigner(priv), newInMemoryBlessingStore(pub), newInMemoryBlessingRoots())
}

// LoadPersistentPrincipal reads state for a principal (private key, BlessingRoots, BlessingStore)
// from the provided directory 'dir' and commits all state changes to the same directory.
// If private key file does not exist then an error 'err' is returned such that os.IsNotExist(err) is true.
// If private key file exists then 'passphrase' must be non-nil if the contents of the file are encrypted,
// otherwise a MissingPassphraseErr is returned.
func LoadPersistentPrincipal(dir string, passphrase []byte) (security.Principal, error) {
	key, err := loadKeyFromDir(dir, passphrase)
	if err != nil {
		return nil, err
	}
	return newPersistentPrincipalFromKey(key, dir)
}

// CreatePersistentPrincipal creates a new principal (private key, BlessingRoots, BlessingStore) and commits all state changes to the provided directory.
// The generated private key is serialized and saved encrypted if the 'passphrase' is non-nil, and unencrypted otherwise.
// If the directory has any preexisting key, CreatePersistentPrincipal will return an error.
// The specified directory may not exist, in which case it gets created by this function.
func CreatePersistentPrincipal(dir string, passphrase []byte) (principal security.Principal, err error) {
	if finfo, err := os.Stat(dir); err == nil {
		if !finfo.IsDir() {
			return nil, fmt.Errorf("%q is not a directory", dir)
		}
	} else if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create %q: %v", dir, err)
	}
	key, err := initKey(dir, nil)
	if err != nil {
		return nil, fmt.Errorf("could not initialize private key from credentials directory %v: %v", dir, err)
	}
	p, err := newPersistentPrincipalFromKey(key, dir)
	return p, err
}

func loadKeyFromDir(dir string, passphrase []byte) (*ecdsa.PrivateKey, error) {
	keyFile := path.Join(dir, privateKeyFile)
	f, err := os.Open(keyFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	key, err := loadPEMKey(f, passphrase)
	if err != nil {
		return nil, err
	}
	return key.(*ecdsa.PrivateKey), nil
}

func newPersistentPrincipalFromKey(key *ecdsa.PrivateKey, dir string) (security.Principal, error) {
	signer, err := security.CreatePrincipal(security.NewInMemoryECDSASigner(key), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create serialization.Signer: %v", err)
	}
	roots, err := newPersistingBlessingRoots(dir, signer)
	if err != nil {
		return nil, fmt.Errorf("failed to load BlessingRoots from %q: %v", dir, err)
	}
	store, err := newPersistingBlessingStore(dir, signer)
	if err != nil {
		return nil, fmt.Errorf("failed to load BlessingStore from %q: %v", dir, err)
	}
	return security.CreatePrincipal(security.NewInMemoryECDSASigner(key), store, roots)
}

// newKey generates an ECDSA (public, private) key pair.
func newKey() (security.PublicKey, *ecdsa.PrivateKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return security.NewECDSAPublicKey(&priv.PublicKey), priv, nil
}

func initKey(dir string, passphrase []byte) (*ecdsa.PrivateKey, error) {
	keyFile := path.Join(dir, privateKeyFile)
	// O_EXCL returns an error if file exists.
	f, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open %q for writing: %v", keyFile, err)
	}
	defer f.Close()
	_, key, err := newKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %v", err)
	}
	if err := savePEMKey(f, key, passphrase); err != nil {
		return nil, fmt.Errorf("failed to save private key to %q: %v", keyFile, err)
	}
	return key, nil
}

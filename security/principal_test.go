package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func TestLoadPersistentPrincipal(t *testing.T) {
	// If the directory does not exist want os.IsNotExists.
	_, err := LoadPersistentPrincipal("/tmp/fake/path/", nil)
	if !os.IsNotExist(err) {
		t.Errorf("invalid path should return does not exist error, instead got %v", err)
	}
	// If the key file exists and is unencrypted we should succeed.
	dir := generatePEMFile(nil)
	if _, err = LoadPersistentPrincipal(dir, nil); err != nil {
		t.Errorf("unencrypted LoadPersistentPrincipal should have succeeded: %v", err)
	}
	os.RemoveAll(dir)

	// If the private key file exists and is encrypted we should succeed with correct passphrase.
	passphrase := []byte("passphrase")
	incorrect_passphrase := []byte("incorrect_passphrase")
	dir = generatePEMFile(passphrase)
	if _, err = LoadPersistentPrincipal(dir, passphrase); err != nil {
		t.Errorf("encrypted LoadPersistentPrincipal should have succeeded: %v", err)
	}
	// and fail with an incorrect passphrase.
	if _, err = LoadPersistentPrincipal(dir, incorrect_passphrase); err == nil {
		t.Errorf("encrypted LoadPersistentPrincipal with incorrect passphrase should fail")
	}
	// and return PassphraseError if the passphrase is nil.
	if _, err = LoadPersistentPrincipal(dir, nil); err != PassphraseErr {
		t.Errorf("encrypted LoadPersistentPrincipal with nil passphrase should return PassphraseErr: %v", err)
	}
	os.RemoveAll(dir)
}

func TestCreatePersistentPrincipal(t *testing.T) {
	tests := []struct {
		Message, Passphrase []byte
	}{
		{[]byte("unencrypted"), nil},
		{[]byte("encrypted"), []byte("passphrase")},
	}
	for _, test := range tests {
		testCreatePersistentPrincipal(t, test.Message, test.Passphrase)
	}
}

func testCreatePersistentPrincipal(t *testing.T, message, passphrase []byte) {
	// Persistence of the BlessingRoots and BlessingStore objects is
	// tested in other files. Here just test the persistence of the key.
	dir, err := ioutil.TempDir("", "TestCreatePersistentPrincipal")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	p, err := CreatePersistentPrincipal(dir, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	p, err = CreatePersistentPrincipal(dir, passphrase)
	if err == nil {
		t.Error("CreatePersistentPrincipal passed unexpectedly")
	}
	p, err = CreateOrOverwritePersistentPrincipal(dir, passphrase)
	if err != nil {
		t.Errorf("CreateOrOverwritePersistentPrincipal failed unexpectedly: %v", err)
	}

	sig, err := p.Sign(message)
	if err != nil {
		t.Fatal(err)
	}

	p2, err := LoadPersistentPrincipal(dir, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if !sig.Verify(p2.PublicKey(), message) {
		t.Errorf("%s failed: p.PublicKey=%v, p2.PublicKey=%v", message, p.PublicKey(), p2.PublicKey())
	}
}

func generatePEMFile(passphrase []byte) (dir string) {
	dir, err := ioutil.TempDir("", "TestLoadPersistentPrincipal")
	if err != nil {
		panic(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	f, err := os.Create(path.Join(dir, privateKeyFile))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err = savePEMKey(f, key, passphrase); err != nil {
		panic(err)
	}
	return dir
}

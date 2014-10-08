package security

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestNewPersistentPrincipal(t *testing.T) {
	// Persistence of the BlessingRoots and BlessingStore objects is
	// tested in other files. Here just test the persistence of the key.
	dir, err := ioutil.TempDir("", "TestNewPersistentPrincipal")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	p, existed, err := NewPersistentPrincipal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Fatalf("%q already has data", existed)
	}
	message := []byte("this is a test message")
	sig, err := p.Sign(message)
	if err != nil {
		t.Fatal(err)
	}

	p2, _, err := NewPersistentPrincipal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !sig.Verify(p2.PublicKey(), message) {
		t.Errorf("p.PublicKey=%v, p2.PublicKey=%v", p.PublicKey(), p2.PublicKey())
	}
}

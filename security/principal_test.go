package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"

	"v.io/core/veyron2/security"
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
	_, err = CreatePersistentPrincipal(dir, passphrase)
	if err == nil {
		t.Error("CreatePersistentPrincipal passed unexpectedly")
	}

	sig, err := p.Sign(message)
	if err != nil {
		t.Fatal(err)
	}

	p2, err := LoadPersistentPrincipal(dir, passphrase)
	if err != nil {
		t.Fatalf("%s failed: %v", message, err)
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
	if err = SavePEMKey(f, key, passphrase); err != nil {
		panic(err)
	}
	return dir
}

func TestPrincipalBlessingsByName(t *testing.T) {
	var p1, p2, p3 security.Principal
	var err error

	if p1, err = NewPrincipal(); err != nil {
		t.Fatal(err)
	}
	if p2, err = NewPrincipal(); err != nil {
		t.Fatal(err)
	}
	alice, err := p1.BlessSelf("alice")
	if err != nil {
		t.Fatal(err)
	}
	p2.AddToRoots(alice)
	var aliceworkfriend, alicegymfriend, aliceworkboss security.Blessings

	if aliceworkfriend, err = p1.Bless(p2.PublicKey(), alice, "work/friend", security.UnconstrainedUse()); err != nil {
		t.Errorf("Bless(work/friend) failed: %v", err)
	}
	p2.BlessingStore().Set(aliceworkfriend, "alice/work/friend")
	if alicegymfriend, err = p1.Bless(p2.PublicKey(), alice, "gym/friend", security.UnconstrainedUse()); err != nil {
		t.Errorf("Bless(gym/friend) failed: %v", err)
	}
	p2.BlessingStore().Set(alicegymfriend, "alice/gym/friend")
	if aliceworkboss, err = p1.Bless(p2.PublicKey(), alice, "work/boss", security.UnconstrainedUse()); err != nil {
		t.Errorf("Bless(work/friend) failed: %v", err)
	}
	p2.BlessingStore().Set(aliceworkboss, "alice/work/boss")

	// Blessing from an untrusted principal that should never be returned
	if p3, err = NewPrincipal(); err != nil {
		t.Fatal(err)
	}
	fake, err := p3.BlessSelf("alice")
	if err != nil {
		t.Fatal(err)
	}
	fakefriend, err := p3.Bless(p2.PublicKey(), fake, "work/friend", security.UnconstrainedUse())
	if err != nil {
		t.Errorf("Bless(work/friend) failed: %v", err)
	}
	_, err = p2.BlessingStore().Set(fakefriend, "fake/work/friend")

	tests := []struct {
		matched []security.Blessings
		pattern security.BlessingPattern
	}{
		{
			matched: []security.Blessings{aliceworkfriend, aliceworkboss},
			pattern: "alice/work/...",
		},
		{
			matched: []security.Blessings{aliceworkfriend},
			pattern: "alice/work/friend",
		},
		{
			matched: []security.Blessings{alicegymfriend},
			pattern: "alice/gym/friend",
		},
		{
			matched: []security.Blessings{aliceworkfriend, alicegymfriend, aliceworkboss},
			pattern: "alice/...",
		},
		{
			matched: []security.Blessings{aliceworkfriend, alicegymfriend, aliceworkboss},
			pattern: "...",
		},
		{
			matched: nil,
			pattern: "alice/school/...",
		},
	}

	for _, test := range tests {
		matched := p2.BlessingsByName(test.pattern)
		if len(matched) != len(test.matched) {
			t.Errorf("BlessingsByName(%s) did not return expected number of matches wanted:%d got:%d", test.pattern, len(test.matched), len(matched))
		}
		for _, m := range matched {
			found := false
			for _, tm := range test.matched {
				if reflect.DeepEqual(m, tm) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Invalid blessing was returned as a match:%v for pattern:%s", m, test.pattern)
			}
		}
	}
}

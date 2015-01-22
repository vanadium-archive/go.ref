package security

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"reflect"
	"testing"

	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vdl"
)

func TestLoadSavePEMKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed ecdsa.GenerateKey: %v", err)
	}

	var buf bytes.Buffer
	if err := SavePEMKey(&buf, key, nil); err != nil {
		t.Fatalf("Failed to save ECDSA private key: %v", err)
	}

	loadedKey, err := LoadPEMKey(&buf, nil)
	if !reflect.DeepEqual(loadedKey, key) {
		t.Fatalf("Got key %v, but want %v", loadedKey, key)
	}
}

func TestLoadSavePEMKeyWithPassphrase(t *testing.T) {
	pass := []byte("openSesame")
	incorrect_pass := []byte("wrongPassphrase")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed ecdsa.GenerateKey: %v", err)
	}
	var buf bytes.Buffer

	// Test incorrect passphrase.
	if err := SavePEMKey(&buf, key, pass); err != nil {
		t.Fatalf("Failed to save ECDSA private key: %v", err)
	}
	loadedKey, err := LoadPEMKey(&buf, incorrect_pass)
	if loadedKey != nil && err != nil {
		t.Errorf("expected (nil, err != nil) received (%v,%v)", loadedKey, err)
	}

	// Test correct password.
	if err := SavePEMKey(&buf, key, pass); err != nil {
		t.Fatalf("Failed to save ECDSA private key: %v", err)
	}
	loadedKey, err = LoadPEMKey(&buf, pass)
	if !reflect.DeepEqual(loadedKey, key) {
		t.Fatalf("Got key %v, but want %v", loadedKey, key)
	}

	// Test nil passphrase.
	if err := SavePEMKey(&buf, key, pass); err != nil {
		t.Fatalf("Failed to save ECDSA private key: %v", err)
	}
	if loadedKey, err = LoadPEMKey(&buf, nil); loadedKey != nil || err != PassphraseErr {
		t.Fatalf("expected(nil, PassphraseError), instead got (%v, %v)", loadedKey, err)
	}
}

// fpCaveat implements security.CaveatValidator.
type fpCaveat struct{}

func (fpCaveat) Validate(security.Context) error { return nil }

// tpCaveat implements security.ThirdPartyCaveat.
type tpCaveat struct{}

func (tpCaveat) Validate(security.Context) (err error)             { return }
func (tpCaveat) ID() (id string)                                   { return }
func (tpCaveat) Location() (loc string)                            { return }
func (tpCaveat) Requirements() (r security.ThirdPartyRequirements) { return }
func (tpCaveat) Dischargeable(security.Context) (err error)        { return }

func TestCaveatUtil(t *testing.T) {
	type C []security.Caveat
	type V []security.CaveatValidator
	type TP []security.ThirdPartyCaveat

	newCaveat := func(v security.CaveatValidator) security.Caveat {
		c, err := security.NewCaveat(v)
		if err != nil {
			t.Fatalf("failed to create Caveat from validator %T: %v", v, c)
		}
		return c
	}

	var (
		fp      fpCaveat
		tp      tpCaveat
		invalid = security.Caveat{ValidatorVOM: []byte("invalid")}
	)
	testdata := []struct {
		caveats    []security.Caveat
		validators []security.CaveatValidator
		tpCaveats  []security.ThirdPartyCaveat
	}{
		{nil, nil, nil},
		{C{newCaveat(fp)}, V{fp}, nil},
		{C{newCaveat(tp)}, V{tp}, TP{tp}},
		{C{newCaveat(fp), newCaveat(tp)}, V{fp, tp}, TP{tp}},
	}
	for _, d := range testdata {
		// Test ThirdPartyCaveats.
		if got := ThirdPartyCaveats(d.caveats...); !reflect.DeepEqual(got, d.tpCaveats) {
			t.Errorf("ThirdPartyCaveats(%v): got: %#v, want: %#v", d.caveats, got, d.tpCaveats)
			continue
		}
		if got := ThirdPartyCaveats(append(d.caveats, invalid)...); !reflect.DeepEqual(got, d.tpCaveats) {
			t.Errorf("ThirdPartyCaveats(%v, invalid): got: %#v, want: %#v", d.caveats, got, d.tpCaveats)
			continue
		}
	}
}

func init() {
	vdl.Register(&fpCaveat{})
	vdl.Register(&tpCaveat{})
}

package principal

import (
	"bytes"
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/security"
	verror "veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vom2"
)

func accountBlessing(p security.Principal, name string) security.Blessings {
	// Ideally the account blessing would be from a principal representing
	// the identity provider but for testing purpose we mock it out using a
	// self-blessing.
	return blessSelf(p, name)
}

type tester struct {
	googleAccount, facebookAccount, origin string
	googleBlessings, facebookBlessings     security.Blessings
}

func (t *tester) testSetters(m *PrincipalManager) error {
	// Test AddAccount.
	if err := m.AddAccount(t.googleAccount, t.googleBlessings); err != nil {
		return fmt.Errorf("AddAccount failed: %v", err)
	}
	if err := m.AddAccount(t.facebookAccount, t.facebookBlessings); err != nil {
		return fmt.Errorf("AddAccount failed: %v", err)
	}

	// Test AddOrigin.
	cav, err := security.MethodCaveat("Foo")
	if err != nil {
		return fmt.Errorf("security.MethodCaveat failed: %v", err)
	}

	if err := m.AddOrigin(t.origin, t.googleAccount, []security.Caveat{cav}); err != nil {
		return fmt.Errorf("AddOrigin failed: %v", err)
	}

	if err := matchesErrorID(m.AddOrigin(t.origin, "nonExistingAccount", nil), errUnknownAccount.ID); err != nil {
		return fmt.Errorf("AddOrigin(..., 'nonExistingAccount', ...): %v", err)
	}
	return nil
}

func (t *tester) testGetters(m *PrincipalManager) error {
	// Test Principal.
	pOrigin, err := m.Principal(t.origin)
	if err != nil {
		return fmt.Errorf("Principal failed: %v", err)
	}

	bOrigin := pOrigin.BlessingStore().Default()
	// Validate the integrity of the bits.
	buf := new(bytes.Buffer)

	encoder, err := vom2.NewBinaryEncoder(buf)

	if err != nil {
		return err
	}

	if encoder.Encode(security.MarshalBlessings(bOrigin)); err != nil {
		return err
	}
	var wire security.WireBlessings
	decoder, err := vom2.NewDecoder(buf)

	if err != nil {
		return err
	}

	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	decoded, err := security.NewBlessings(wire)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(decoded, bOrigin) {
		return fmt.Errorf("reflect.DeepEqual(%v, %v) failed after validBlessing", decoded, bOrigin)
	}
	ctx := func(method string) security.Context {
		return security.NewContext(&security.ContextParams{LocalPrincipal: pOrigin, Method: method})
	}

	// Validate the blessings in various contexts.
	want := []string{t.googleAccount + security.ChainSeparator + url.QueryEscape(t.origin)}
	if got := bOrigin.ForContext(ctx("Foo")); !reflect.DeepEqual(got, want) {
		return fmt.Errorf("with method 'Foo', got blessing: %v, want: %v", got, want)
	}
	if got := bOrigin.ForContext(ctx("Bar")); got != nil {
		return fmt.Errorf("with method 'Bar', got blessing: %v, want nil", got)
	}

	unknownOrigin := "http://unknown.com:80"
	_, err = m.Principal(unknownOrigin)
	if merr := matchesErrorID(err, verror.NoExist.ID); merr != nil {
		return fmt.Errorf("Principal(%v): %v, errorid=%v", unknownOrigin, merr)
	}

	// Test BlessingsForAccount.
	if got, err := m.BlessingsForAccount(t.googleAccount); err != nil || !reflect.DeepEqual(got, t.googleBlessings) {
		return fmt.Errorf("BlessingsForAccount(%v): got: %v, %v want: %v, nil", t.googleAccount, got, err, t.googleBlessings)
	}
	if got, err := m.BlessingsForAccount(t.facebookAccount); err != nil || !reflect.DeepEqual(got, t.facebookBlessings) {
		return fmt.Errorf("BlessingsForAccount(%v): got: %v, %v, want: %v, nil", t.facebookAccount, got, err, t.facebookBlessings)
	}
	nonExistingAccount := "nonExistingAccount"
	if got, err := m.BlessingsForAccount(nonExistingAccount); got != nil {
		return fmt.Errorf("BlessingsForAccount(%v): got: %v, want nil", nonExistingAccount, got)
	} else if merr := matchesError(err, "unknown account"); merr != nil {
		return fmt.Errorf("BlessingsForAccount(%v) returned error: %v", nonExistingAccount, merr)
	}
	return nil
}

func newTester(root security.Principal) *tester {
	googleAccount := "google/alice@gmail.com"
	facebookAccount := "facebook/alice@facebook.com"
	return &tester{
		googleAccount:     googleAccount,
		facebookAccount:   facebookAccount,
		origin:            "https://sampleapp-1.com:443",
		googleBlessings:   accountBlessing(root, googleAccount),
		facebookBlessings: accountBlessing(root, facebookAccount),
	}
}

func TestPrincipalManager(t *testing.T) {
	root := newPrincipal()
	m, err := NewPrincipalManager(root, &InMemorySerializer{})
	if err != nil {
		t.Fatalf("NewPrincipalManager failed: %v", err)
	}

	mt := newTester(root)
	if err := mt.testSetters(m); err != nil {
		t.Fatal(err)
	}
	if err := mt.testGetters(m); err != nil {
		t.Fatal(err)
	}
}

func TestPrincipalManagerPersistence(t *testing.T) {
	root := newPrincipal()
	serializer := &InMemorySerializer{}
	m, err := NewPrincipalManager(root, serializer)
	if err != nil {
		t.Fatalf("NewPrincipalManager failed: %v", err)
	}

	mt := newTester(root)
	if err := mt.testSetters(m); err != nil {
		t.Fatal(err)
	}
	if err := mt.testGetters(m); err != nil {
		t.Fatal(err)
	}

	if m, err = NewPrincipalManager(root, serializer); err != nil {
		t.Fatalf("NewPrincipalManager failed: %v", err)
	}
	if err := mt.testGetters(m); err != nil {
		t.Fatal(err)
	}
}

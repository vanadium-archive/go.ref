package principal

import (
	"bytes"
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"

	"v.io/core/veyron2/security"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vom"
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
		return fmt.Errorf("AddAccount(%v, %v) failed: %v", t.googleAccount, t.googleBlessings, err)
	}
	if err := m.AddAccount(t.facebookAccount, t.facebookBlessings); err != nil {
		return fmt.Errorf("AddAccount(%v, %v) failed: %v", t.facebookAccount, t.facebookBlessings, err)
	}

	// Test AddOrigin.
	cav, err := security.MethodCaveat("Foo")
	if err != nil {
		return fmt.Errorf("security.MethodCaveat failed: %v", err)
	}

	if err := m.AddOrigin(t.origin, t.googleAccount, []security.Caveat{cav}, nil); err != nil {
		return fmt.Errorf("AddOrigin failed: %v", err)
	}

	if err := matchesErrorID(m.AddOrigin(t.origin, "nonExistingAccount", nil, nil), errUnknownAccount.ID); err != nil {
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

	encoder, err := vom.NewBinaryEncoder(buf)

	if err != nil {
		return err
	}

	if encoder.Encode(security.MarshalBlessings(bOrigin)); err != nil {
		return err
	}
	var wire security.WireBlessings
	decoder, err := vom.NewDecoder(buf)

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

func TestOriginHasAccount(t *testing.T) {
	root := newPrincipal()
	m, err := NewPrincipalManager(root, &InMemorySerializer{})
	if err != nil {
		t.Fatalf("NewPrincipalManager failed: %v", err)
	}
	googleAccount := "fred/jim@gmail.com"

	// Test with unknown origin.
	unknownOrigin := "http://unknown.com"
	if m.OriginHasAccount(unknownOrigin) {
		fmt.Errorf("Expected m.OriginHasAccount(%v) to be false but it was true.", unknownOrigin)
	}

	// Test with no expiration caveat.
	origin1 := "http://origin-1.com"

	methodCav, err := security.MethodCaveat("Foo")
	if err != nil {
		fmt.Errorf("security.MethodCaveat failed: %v", err)
	}

	if err := m.AddOrigin(origin1, googleAccount, []security.Caveat{methodCav}, nil); err != nil {
		fmt.Errorf("AddOrigin failed: %v", err)
	}

	if !m.OriginHasAccount(origin1) {
		fmt.Errorf("Expected m.OriginHasAccount(%v) to be true but it was false.", origin1)
	}

	// Test with expiration caveat in the future.
	origin2 := "http://origin-2.com"
	futureTime := time.Now().Add(5 * time.Minute)

	futureExpCav, err := security.ExpiryCaveat(futureTime)
	if err != nil {
		fmt.Errorf("security.ExpiryCaveat(%v) failed: %v", futureTime, err)
	}

	if err := m.AddOrigin(origin2, googleAccount, []security.Caveat{futureExpCav}, []time.Time{futureTime}); err != nil {
		fmt.Errorf("AddOrigin failed: %v", err)
	}

	if !m.OriginHasAccount(origin2) {
		fmt.Errorf("Expected m.OriginHasAccount(%v) to be true but it was false.", origin2)
	}

	// Test with expiration caveats in the past and future.
	origin3 := "http://origin-3.com"
	pastTime := time.Now().Add(-5 * time.Minute)

	pastExpCav, err := security.ExpiryCaveat(pastTime)
	if err != nil {
		fmt.Errorf("security.ExpiryCaveat(%v) failed: %v", pastTime, err)
	}

	if err := m.AddOrigin(origin3, googleAccount, []security.Caveat{futureExpCav, pastExpCav}, []time.Time{futureTime, pastTime}); err != nil {
		fmt.Errorf("AddOrigin failed: %v", err)
	}

	if m.OriginHasAccount(origin3) {
		fmt.Errorf("Expected m.OriginHasAccount(%v) to be false but it was true.", origin3)
	}
}

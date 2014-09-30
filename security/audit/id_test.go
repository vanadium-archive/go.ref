package audit_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"veyron.io/veyron/veyron/security/audit"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
)

func TestAuditingID(t *testing.T) {
	var (
		// A bunch of  values that will be used as arguments to method calls.
		byteSlice        []byte
		publicID         = security.FakePublicID("publicid")
		str              string
		duration         time.Duration
		caveats          []security.Caveat
		thirdPartyCaveat thirdPartyCaveat
		context          context

		// A bunch of values that will be returned as results
		rSignature = security.Signature{R: []byte{1}, S: []byte{1}}
		rBlessing  = security.FakePublicID("blessed")
		rDischarge = new(discharge)

		// The error returned by call calls to mockID operations
		wantErr = errors.New("call failed")

		// The PrivateID to wrap over
		mockID      = new(mockID)
		mockAuditor = new(mockAuditor)
	)
	id := audit.NewPrivateID(mockID, mockAuditor)
	tests := []struct {
		Method        string
		Args          V
		Result        interface{} // Result returned by the Method call.
		AuditedResult interface{} // Result written to the audit entry.
	}{
		{"Sign", V{byteSlice}, rSignature, nil},
		{"Bless", V{publicID, str, duration, caveats}, rBlessing, rBlessing},
		{"MintDischarge", V{thirdPartyCaveat, context, duration, caveats}, rDischarge, nil},
	}
	for _, test := range tests {
		// Test1: Nothing is written to the audit log if the underlying operation fails.
		mockID.NextError = wantErr
		results, err := call(id, test.Method, test.Args)
		if err != nil {
			t.Errorf("failed to invoke id.%v(%#v): %v", test.Method, test.Args, err)
			continue
		}
		got, ok := results[len(results)-1].(error)
		if !ok || got != wantErr {
			t.Errorf("id.%v(%#v) returned (..., %v), want (..., %v)", test.Method, test.Args, got, wantErr)
		}
		// Nothing should be audited
		if audited := mockAuditor.Release(); !reflect.DeepEqual(audited, audit.Entry{}) {
			t.Errorf("id.%v(%#v) resulted in [%+v] being written to the audit log, nothing should have been", test.Method, test.Args, audited)
		}

		// Test2: If the auditing fails then the operation should fail too.
		mockAuditor.NextError = errors.New("auditor failed")
		results, err = call(id, test.Method, test.Args)
		if err != nil {
			t.Errorf("failed to invoke id.%v(%#v): %v", test.Method, test.Args, err)
			continue
		}
		got, ok = results[len(results)-1].(error)
		if !ok || !strings.HasSuffix(got.Error(), "auditor failed") {
			t.Errorf("id.%v(%#v) returned (..., %v) when auditor failed, wanted (..., %v)", test.Method, test.Args, got, "... auditor failed")
		}

		// Test3: Should audit the call and return the same value as the underlying operation.
		now := time.Now()
		mockID.NextResult = test.Result
		results, err = call(id, test.Method, test.Args)
		audited := mockAuditor.Release()
		if err != nil {
			t.Errorf("failed to invoke id.%v(%#v): %v", test.Method, test.Args, err)
			continue
		}
		if got := results[len(results)-1]; got != nil {
			t.Errorf("id.%v(%#v) returned an error: %v", test.Method, test.Args, got)
		}
		if got := results[0]; !reflect.DeepEqual(got, test.Result) {
			t.Errorf("id.%v(%#v) returned %v(%T) want %v(%T)", test.Method, test.Args, got, got, test.Result, test.Result)
		}
		if audited.Timestamp.Before(now) || audited.Timestamp.IsZero() {
			t.Errorf("id.%v(%#v) audited the time as %v, should have been a time after %v", test.Method, test.Args, audited.Timestamp, now)
		}
		if want := (audit.Entry{
			Method:    test.Method,
			Arguments: []interface{}(test.Args),
			Results:   sliceOrNil(test.AuditedResult),
			Timestamp: audited.Timestamp, // Hard to come up with the expected timestamp, relying on sanity check above.
		}); !reflect.DeepEqual(audited, want) {
			t.Errorf("id.%v(%#v) resulted in [%#v] being audited, wanted [%#v]", test.Method, test.Args, audited, want)
		}
	}
}

func sliceOrNil(item interface{}) []interface{} {
	if item == nil {
		return nil
	}
	return []interface{}{item}
}

func TestUnauditedMethods(t *testing.T) {
	var (
		mockID      = new(mockID)
		mockAuditor = new(mockAuditor)
		id          = audit.NewPrivateID(mockID, mockAuditor)
		key, err    = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		pubkey      = security.NewECDSAPublicKey(&key.PublicKey)
		emptyEntry  audit.Entry
	)
	if err != nil {
		t.Fatal(err)
	}
	mockID.NextResult = pubkey
	if got, want := id.PublicKey(), pubkey; got != want {
		t.Errorf("Got %v want %v", got, want)
	}
	if entry := mockAuditor.Release(); !reflect.DeepEqual(entry, emptyEntry) {
		t.Errorf("Unexpected audit: %v", entry)
	}

	var pubID security.PublicID
	mockID.NextResult = pubID
	if got, want := id.PublicID(), pubID; got != want {
		t.Errorf("Got %v want %v", got, want)
	}
	if entry := mockAuditor.Release(); !reflect.DeepEqual(entry, emptyEntry) {
		t.Errorf("Unexpected audit: %v", entry)
	}
}

func TestDerive(t *testing.T) {
	var (
		mockID      = new(mockID)
		mockAuditor = new(mockAuditor)
		id          = audit.NewPrivateID(mockID, mockAuditor)
		publicID    = security.FakePublicID("publicid")
		rSignature  = security.Signature{R: []byte{2}, S: []byte{2}}
		msg         = []byte{1, 2, 3}
	)
	mockID.NextResult = mockID
	derived, err := id.Derive(publicID)
	if err != nil {
		t.Fatal(err)
	}

	mockID.NextResult = rSignature
	if got, err := derived.Sign(msg); err != nil || !reflect.DeepEqual(got, rSignature) {
		t.Fatalf("Got (%v, %v) want (%v, nil)", got, err, rSignature)
	}
	got := mockAuditor.Release()
	if want := (audit.Entry{Method: "Sign", Arguments: []interface{}{msg}, Timestamp: got.Timestamp}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Audit log shows [%v] want [%v]", got, want)
	}
}

type mockID struct {
	NextError  error
	NextResult interface{}
}

func (id *mockID) Sign(message []byte) (security.Signature, error) {
	defer id.reset()
	ret, ok := id.NextResult.(security.Signature)
	if ok {
		return ret, id.NextError
	}
	return security.Signature{}, id.NextError
}

func (id *mockID) PublicID() security.PublicID {
	defer id.reset()
	if id.NextResult == nil {
		return nil
	}
	return id.NextResult.(security.PublicID)
}
func (id *mockID) Bless(blessee security.PublicID, blessingName string, duration time.Duration, caveats []security.Caveat) (security.PublicID, error) {
	defer id.reset()
	result, _ := id.NextResult.(security.PublicID)
	return result, id.NextError
}
func (id *mockID) Derive(publicID security.PublicID) (security.PrivateID, error) {
	defer id.reset()
	result, _ := id.NextResult.(security.PrivateID)
	return result, id.NextError
}
func (id *mockID) MintDischarge(caveat security.ThirdPartyCaveat, context security.Context, duration time.Duration, caveats []security.Caveat) (security.Discharge, error) {
	defer id.reset()
	result, _ := id.NextResult.(security.Discharge)
	return result, id.NextError
}

func (id *mockID) reset() {
	id.NextError = nil
	id.NextResult = nil
}

func (id *mockID) PublicKey() security.PublicKey { return id.NextResult.(security.PublicKey) }

type mockAuditor struct {
	LastEntry audit.Entry
	NextError error
}

func (a *mockAuditor) Audit(entry audit.Entry) error {
	if a.NextError != nil {
		err := a.NextError
		a.NextError = nil
		return err
	}
	a.LastEntry = entry
	return nil
}

func (a *mockAuditor) Release() audit.Entry {
	entry := a.LastEntry
	a.LastEntry = audit.Entry{}
	return entry
}

type V []interface{}

// thirdPartyCaveat implements security.ThirdPartyCaveat
type thirdPartyCaveat struct{}

func (thirdPartyCaveat) Validate(security.Context) error { return nil }
func (thirdPartyCaveat) ID() string                      { return "thirdPartyCaveatID" }
func (thirdPartyCaveat) Location() string                { return "thirdPartyCaveatLocation" }
func (thirdPartyCaveat) Requirements() security.ThirdPartyRequirements {
	return security.ThirdPartyRequirements{}
}
func (thirdPartyCaveat) Dischargeable(security.Context) error { return nil }

// context implements security.Context
type context struct{}

func (context) Method() string                            { return "method" }
func (context) Name() string                              { return "name" }
func (context) Suffix() string                            { return "suffix" }
func (context) Label() security.Label                     { return security.ReadLabel }
func (context) Discharges() map[string]security.Discharge { return nil }
func (context) LocalID() security.PublicID                { return nil }
func (context) RemoteID() security.PublicID               { return nil }
func (context) LocalPrincipal() security.Principal        { return nil }
func (context) LocalBlessings() security.Blessings        { return nil }
func (context) RemoteBlessings() security.Blessings       { return nil }
func (context) LocalEndpoint() naming.Endpoint            { return nil }
func (context) RemoteEndpoint() naming.Endpoint           { return nil }

// discharge implements the security.Discharge interface
type discharge struct{}

func (*discharge) ID() string                                     { return "thirdPartyCaveatID" }
func (*discharge) ThirdPartyCaveats() []security.ThirdPartyCaveat { return nil }

func call(receiver interface{}, method string, args V) (results []interface{}, err interface{}) {
	defer func() {
		err = recover()
	}()
	callargs := make([]reflect.Value, len(args))
	for idx, arg := range args {
		callargs[idx] = reflect.ValueOf(arg)
	}
	callresults := reflect.ValueOf(receiver).MethodByName(method).Call(callargs)
	results = make([]interface{}, len(callresults))
	for idx, res := range callresults {
		results[idx] = res.Interface()
	}
	return
}

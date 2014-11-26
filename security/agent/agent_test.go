package agent_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"reflect"
	"testing"

	_ "veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/security/agent"
	"veyron.io/veyron/veyron/security/agent/server"

	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror2"
)

func setupAgent(t *testing.T, p security.Principal) security.Principal {
	runtime := rt.Init()
	sock, err := server.RunAnonymousAgent(runtime, p)
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()
	client, err := runtime.NewClient(options.VCSecurityNone)
	if err != nil {
		t.Fatal(err)
	}
	var agentP security.Principal
	if agentP, err = agent.NewAgentPrincipal(client, int(sock.Fd()), runtime.NewContext()); err != nil {
		t.Fatal(err)
	}
	return agentP
}

type testInfo struct {
	Method string
	Args   V
	Result interface{} // Result returned by the Method call.
	Error  verror2.E   // If Error is not nil will be compared to the last result.
}

const pkgPath = "veyron.io/veyron/veyron/security/agent/"

var (
	addToRootsErr      = verror2.Register(pkgPath+".addToRoots", verror2.NoRetry, "")
	storeSetDefaultErr = verror2.Register(pkgPath+".storeSetDefault", verror2.NoRetry, "")
	rootsAddErr        = verror2.Register(pkgPath+".rootsAdd", verror2.NoRetry, "")
	rootsRecognizedErr = verror2.Register(pkgPath+".rootsRecognized", verror2.NoRetry, "")
)

func TestAgent(t *testing.T) {
	var (
		thirdPartyCaveat, discharge = newThirdPartyCaveatAndDischarge(t)
		mockP                       = newMockPrincipal()
		agent                       = setupAgent(t, mockP)
	)
	tests := []testInfo{
		{"BlessSelf", V{"self"}, newBlessing(t, "blessing"), nil},
		{"Bless", V{newPrincipal(t).PublicKey(), newBlessing(t, "root"), "extension", security.UnconstrainedUse()}, newBlessing(t, "root/extension"), nil},
		// TODO(toddw): This change is necessary for vom2:
		//{"Sign", V{make([]byte, 10)}, security.Signature{Purpose: []byte{}, R: []byte{1}, S: []byte{1}}, nil},
		{"Sign", V{make([]byte, 10)}, security.Signature{R: []byte{1}, S: []byte{1}}, nil},
		{"MintDischarge", V{thirdPartyCaveat, security.UnconstrainedUse()}, discharge, nil},
		{"PublicKey", V{}, mockP.PublicKey(), nil},
		{"AddToRoots", V{newBlessing(t, "blessing")}, nil, verror2.Make(addToRootsErr, nil)},
	}
	for _, test := range tests {
		mockP.NextResult = test.Result
		mockP.NextError = test.Error
		runTest(t, agent, test)
	}

	store := agent.BlessingStore()
	storeTests := []testInfo{
		{"Set", V{newBlessing(t, "blessing"), security.BlessingPattern("test")}, newBlessing(t, "root/extension"), nil},
		{"ForPeer", V{"test", "oink"}, newBlessing(t, "for/peer"), nil},
		{"SetDefault", V{newBlessing(t, "blessing")}, nil, verror2.Make(storeSetDefaultErr, nil)},
		{"Default", V{}, newBlessing(t, "root/extension"), nil},
		{"PublicKey", V{}, mockP.PublicKey(), nil},
		{"DebugString", V{}, "StoreString", nil},
	}
	for _, test := range storeTests {
		mockP.MockStore.NextResult = test.Result
		mockP.MockStore.NextError = test.Error
		runTest(t, store, test)
	}

	roots := agent.Roots()
	rootTests := []testInfo{
		{"Add", V{newPrincipal(t).PublicKey(), security.BlessingPattern("test")}, nil, verror2.Make(rootsAddErr, nil)},
		{"Recognized", V{newPrincipal(t).PublicKey(), "blessing"}, nil, verror2.Make(rootsRecognizedErr, nil)},
		{"DebugString", V{}, "RootsString", nil},
	}
	for _, test := range rootTests {
		mockP.MockRoots.NextResult = test.Result
		mockP.MockRoots.NextError = test.Error
		runTest(t, roots, test)
	}
}

func runTest(t *testing.T, receiver interface{}, test testInfo) {
	results, err := call(receiver, test.Method, test.Args)
	if err != nil {
		t.Errorf("failed to invoke p.%v(%#v): %v", test.Method, test.Args, err)
		return
	}
	// We only set the error value when error is the only output to ensure the real function gets called.
	if test.Error != nil {
		if got := results[len(results)-1]; got == nil || !verror2.Is(got.(error), test.Error.ErrorID()) {
			t.Errorf("p.%v(%#v) returned an incorrect error: %v, expected %v", test.Method, test.Args, got, test.Error)
		}
		if len(results) == 1 {
			return
		}
	}
	if got := results[0]; !reflect.DeepEqual(got, test.Result) {
		t.Errorf("p.%v(%#v) returned %#v want %#v", test.Method, test.Args, got, test.Result)
	}
}

func newMockPrincipal() *mockPrincipal {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	pkey := security.NewECDSAPublicKey(&key.PublicKey)
	return &mockPrincipal{
		Key:       pkey,
		MockStore: &mockBlessingStore{Key: pkey},
		MockRoots: &mockBlessingRoots{},
	}
}

type mockPrincipal struct {
	NextError  error
	NextResult interface{}
	Key        security.PublicKey
	MockStore  *mockBlessingStore
	MockRoots  *mockBlessingRoots
}

func (p *mockPrincipal) reset() {
	p.NextError = nil
	p.NextResult = nil
}

func (p *mockPrincipal) Bless(security.PublicKey, security.Blessings, string, security.Caveat, ...security.Caveat) (security.Blessings, error) {
	defer p.reset()
	b, _ := p.NextResult.(security.Blessings)
	return b, p.NextError
}

func (p *mockPrincipal) BlessSelf(string, ...security.Caveat) (security.Blessings, error) {
	defer p.reset()
	b, _ := p.NextResult.(security.Blessings)
	return b, p.NextError
}

func (p *mockPrincipal) Sign([]byte) (sig security.Signature, err error) {
	defer p.reset()
	sig, _ = p.NextResult.(security.Signature)
	err = p.NextError
	return
}

func (p *mockPrincipal) MintDischarge(security.ThirdPartyCaveat, security.Caveat, ...security.Caveat) (security.Discharge, error) {
	defer p.reset()
	d, _ := p.NextResult.(security.Discharge)
	return d, p.NextError
}

func (p *mockPrincipal) PublicKey() security.PublicKey         { return p.Key }
func (p *mockPrincipal) Roots() security.BlessingRoots         { return p.MockRoots }
func (p *mockPrincipal) BlessingStore() security.BlessingStore { return p.MockStore }
func (p *mockPrincipal) AddToRoots(b security.Blessings) error {
	defer p.reset()
	return p.NextError
}

type mockBlessingStore struct {
	NextError  error
	NextResult interface{}
	Key        security.PublicKey
}

func (s *mockBlessingStore) reset() {
	s.NextError = nil
	s.NextResult = nil
}

func (s *mockBlessingStore) Set(security.Blessings, security.BlessingPattern) (security.Blessings, error) {
	defer s.reset()
	b, _ := s.NextResult.(security.Blessings)
	return b, s.NextError
}

func (s *mockBlessingStore) ForPeer(...string) security.Blessings {
	defer s.reset()
	b, _ := s.NextResult.(security.Blessings)
	return b
}

func (s *mockBlessingStore) SetDefault(security.Blessings) error {
	defer s.reset()
	return s.NextError
}

func (s *mockBlessingStore) Default() security.Blessings {
	defer s.reset()
	b, _ := s.NextResult.(security.Blessings)
	return b
}

func (s *mockBlessingStore) PublicKey() security.PublicKey { return s.Key }

func (s *mockBlessingStore) DebugString() string {
	defer s.reset()
	return s.NextResult.(string)
}

type mockBlessingRoots struct {
	NextError  error
	NextResult interface{}
}

func (r *mockBlessingRoots) reset() {
	r.NextError = nil
	r.NextResult = nil
}

func (r *mockBlessingRoots) Add(security.PublicKey, security.BlessingPattern) error {
	defer r.reset()
	return r.NextError
}

func (r *mockBlessingRoots) Recognized(security.PublicKey, string) error {
	defer r.reset()
	return r.NextError
}

func (r *mockBlessingRoots) DebugString() string {
	defer r.reset()
	return r.NextResult.(string)
}

type V []interface{}

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

func newPrincipal(t *testing.T) security.Principal {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer := security.NewInMemoryECDSASigner(key)
	p, err := security.CreatePrincipal(signer, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newCaveat(c security.Caveat, err error) security.Caveat {
	if err != nil {
		panic(err)
	}
	return c
}

func newBlessing(t *testing.T, name string) security.Blessings {
	b, err := newPrincipal(t).BlessSelf(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newThirdPartyCaveatAndDischarge(t *testing.T) (security.ThirdPartyCaveat, security.Discharge) {
	p := newPrincipal(t)
	c, err := security.NewPublicKeyCaveat(p.PublicKey(), "location", security.ThirdPartyRequirements{}, newCaveat(security.MethodCaveat("method")))
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.MintDischarge(c, security.UnconstrainedUse())
	if err != nil {
		t.Fatal(err)
	}
	return c, d
}

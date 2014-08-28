package agent_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"reflect"
	"testing"
	"veyron/security/agent"
	"veyron/security/agent/server"
	"veyron2"
	"veyron2/rt"
	"veyron2/security"
)

type fakesigner struct {
	key *ecdsa.PublicKey
}

type testdata struct {
	server_conn os.File
	agent       security.Signer
	signer      fakesigner
}

func setup() *testdata {
	runtime := rt.Init()
	result := &testdata{signer: newFakeSigner()}
	sock, err := server.RunAnonymousAgent(runtime, result.signer)
	if err != nil {
		panic(err)
	}
	defer sock.Close()
	client, err := runtime.NewClient(veyron2.VCSecurityNone)
	if err != nil {
		panic(err)
	}
	if agent, err := agent.NewAgentSigner(client, agent.CreateAgentEndpoint(int(sock.Fd())), runtime.NewContext()); err == nil {
		result.agent = agent
		return result
	} else {
		panic(err)
	}
}

func TestSignature(t *testing.T) {
	td := setup()
	sig, err := td.agent.Sign([]byte("abc"))

	if err != nil {
		t.Error(err)
	}
	if sig.R[0] != 6 {
		t.Errorf("Bad R, expected [6], got %w", sig.R)
	}
	if sig.S[0] != 7 {
		t.Errorf("Bad S, expected [7], got %w", sig.S)
	}
}

func TestPublicKey(t *testing.T) {
	td := setup()
	expected_key := td.signer.PublicKey()
	agent_key := td.agent.PublicKey()
	if !reflect.DeepEqual(expected_key, agent_key) {
		t.Errorf("Different keys: %v, %v", expected_key, agent_key)
	}
}

func newFakeSigner() fakesigner {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return fakesigner{&key.PublicKey}
}

func (fakesigner) Sign(message []byte) (security.Signature, error) {
	return security.Signature{"", []byte{6}, []byte{7}}, nil
}

func (s fakesigner) PublicKey() *ecdsa.PublicKey {
	return s.key
}

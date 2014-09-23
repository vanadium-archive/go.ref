package agent_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"reflect"
	"testing"
	"veyron.io/veyron/veyron/security/agent"
	"veyron.io/veyron/veyron/security/agent/server"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
)

type fakesigner struct {
	key security.PublicKey
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
	if agent, err := agent.NewAgentSigner(client, int(sock.Fd()), runtime.NewContext()); err == nil {
		result.agent = agent
		return result
	} else {
		panic(err)
	}
}

func TestSignature(t *testing.T) {
	td := setup()
	sig, err := td.agent.Sign(nil, []byte("abc"))

	if err != nil {
		t.Error(err)
	}
	expected := security.Signature{R: []byte{6}, S: []byte{7}}
	if !reflect.DeepEqual(sig, expected) {
		t.Errorf("Bad signature. Got\n%#v\nExpected:\n%#v", sig, expected)
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
	return fakesigner{security.NewECDSAPublicKey(&key.PublicKey)}
}

func (fakesigner) Sign(message []byte) (security.Signature, error) {
	var sig security.Signature
	sig.R, sig.S = []byte{6}, []byte{7}
	return sig, nil
}

func (s fakesigner) PublicKey() security.PublicKey {
	return s.key
}

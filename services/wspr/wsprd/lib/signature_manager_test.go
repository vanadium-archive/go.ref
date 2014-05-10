package lib

import (
	"reflect"
	"testing"

	mocks_ipc "veyron/runtimes/google/testing/mocks/ipc"
	"veyron2/idl"
	"veyron2/ipc"
	"veyron2/wiretype"
)

const (
	name = "/veyron/name"
)

func expectedSignature() ipc.ServiceSignature {
	return ipc.ServiceSignature{
		Methods: make(map[string]ipc.MethodSignature),
		TypeDefs: []idl.AnyData{
			wiretype.NamedPrimitiveType{
				Name: "veyron2/idl.AnyData",
				Type: wiretype.TypeIDInterface,
			},
		},
	}
}

func client() *mocks_ipc.SimpleMockClient {
	return mocks_ipc.NewSimpleClient(
		map[string][]interface{}{
			signatureMethodName: []interface{}{expectedSignature(), nil},
		},
	)
}

func assertMethodSignatureAsExpected(t *testing.T, got, expected ipc.MethodSignature) {
	if !reflect.DeepEqual(got.InArgs, expected.InArgs) {
		t.Errorf(`InArgs do not match: result "%v", want "%v"`, got.InArgs, expected.InArgs)
		return
	}
	if !reflect.DeepEqual(got.OutArgs, expected.OutArgs) {
		t.Errorf(`OutArgs do not match: result "%v", want "%v"`, got.OutArgs, expected.OutArgs)
		return
	}
	if got.InStream != expected.InStream {
		t.Errorf(`InStreams do not match: result "%v", want "%v"`, got.InStream, expected.InStream)
		return
	}
	if got.OutStream != expected.OutStream {
		t.Errorf(`OutStream do not match: result "%v", want "%v"`, got.OutStream, expected.OutStream)
		return
	}
}

func assertSignatureAsExpected(t *testing.T, got, expected *ipc.ServiceSignature) {
	if !reflect.DeepEqual(got.TypeDefs, expected.TypeDefs) {
		t.Errorf(`TypeDefs do not match: result "%v", want "%v"`, got.TypeDefs, expected.TypeDefs)
		return
	}
	if n, m := len(got.Methods), len(expected.Methods); n != m {
		t.Errorf(`Wrong number of signature methods: result "%d", want "%d"`, n, m)
		return
	}
	for gotName, gotMethod := range got.Methods {
		expectedMethod, ok := expected.Methods[gotName]
		if !ok {
			t.Errorf(`Method "%v" was expected but not found`, gotName)
			return
		}

		assertMethodSignatureAsExpected(t, gotMethod, expectedMethod)
	}
}

func TestFetching(t *testing.T) {
	sm := newSignatureManager()
	got, err := sm.signature(name, client())
	if err != nil {
		t.Errorf(`Did not expect an error but got %v`, err)
		return
	}
	expected := expectedSignature()
	assertSignatureAsExpected(t, got, &expected)
}

func TestThatCachedAfterFetching(t *testing.T) {
	sm := newSignatureManager()
	sig, _ := sm.signature(name, client())
	cache, ok := sm.cache[name]
	if !ok {
		t.Errorf(`Signature manager did not cache the results`)
		return
	}
	assertSignatureAsExpected(t, &cache.signature, sig)
}

func TestThatCacheIsUsed(t *testing.T) {
	client := client()
	sm := newSignatureManager()

	// call twice
	sm.signature(name, client)
	sm.signature(name, client)

	// expect number of calls to Signature method of client to still be 1 since cache
	// should have been used despite the second call
	if client.TimesCalled(signatureMethodName) != 1 {
		t.Errorf("Signature cache was not used for the second call")
	}
}

func TestThatLastAccessedGetUpdated(t *testing.T) {
	client := client()
	sm := newSignatureManager()
	sm.signature(name, client)
	prevAccess := sm.cache[name].lastAccessed

	// access again
	sm.signature(name, client)
	newAccess := sm.cache[name].lastAccessed

	if !newAccess.After(prevAccess) {
		t.Errorf("LastAccessed was not updated for cache entry")
	}
}

func TestThatTTLExpires(t *testing.T) {
	client := client()
	sm := newSignatureManager()
	sm.signature(name, client)

	// make last accessed go over the ttl
	sm.cache[name].lastAccessed = sm.cache[name].lastAccessed.Add(-ttl)

	// make a second call
	sm.signature(name, client)

	// expect number of calls to Signature method of client to be 2 since cache should have expired
	if client.TimesCalled(signatureMethodName) != 2 {
		t.Errorf("Cache was still used but TTL had passed. It should have been fetched again")
	}
}

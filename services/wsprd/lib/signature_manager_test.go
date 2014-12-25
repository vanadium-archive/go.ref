package lib

import (
	"reflect"
	"testing"

	_ "v.io/veyron/veyron/profiles"
	mocks_ipc "v.io/veyron/veyron/runtimes/google/testing/mocks/ipc"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/vdl"
	"v.io/veyron/veyron2/vdl/vdlroot/src/signature"
)

const (
	name = "/veyron/name"
)

func wantSignature() []signature.Interface {
	return []signature.Interface{
		{
			Methods: []signature.Method{
				{
					Name:    "Method1",
					InArgs:  []signature.Arg{{Type: vdl.StringType}},
					OutArgs: []signature.Arg{{Type: vdl.ErrorType}},
				},
			},
		},
	}
}

func client() *mocks_ipc.SimpleMockClient {
	return mocks_ipc.NewSimpleClient(
		map[string][]interface{}{
			"__Signature": []interface{}{wantSignature(), nil},
		},
	)
}

func TestFetching(t *testing.T) {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %s", err)
	}
	defer runtime.Cleanup()

	sm := NewSignatureManager()
	got, err := sm.Signature(runtime.NewContext(), name, client())
	if err != nil {
		t.Errorf(`Did not expect an error but got %v`, err)
		return
	}
	if want := wantSignature(); !reflect.DeepEqual(got, want) {
		t.Errorf(`Signature got %v, want %v`, got, want)
	}
}

func TestThatCachedAfterFetching(t *testing.T) {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %s", err)
	}
	defer runtime.Cleanup()

	sm := NewSignatureManager().(*signatureManager)
	sig, _ := sm.Signature(runtime.NewContext(), name, client())
	cache, ok := sm.cache[name]
	if !ok {
		t.Errorf(`Signature manager did not cache the results`)
		return
	}
	if got, want := cache.sig, sig; !reflect.DeepEqual(got, want) {
		t.Errorf(`Cached signature got %v, want %v`, got, want)
	}
}

func TestThatCacheIsUsed(t *testing.T) {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %s", err)
	}
	defer runtime.Cleanup()

	client := client()
	sm := NewSignatureManager()

	// call twice
	sm.Signature(runtime.NewContext(), name, client)
	sm.Signature(runtime.NewContext(), name, client)

	// expect number of calls to Signature method of client to still be 1 since cache
	// should have been used despite the second call
	if client.TimesCalled("__Signature") != 1 {
		t.Errorf("Signature cache was not used for the second call")
	}
}

func TestThatLastAccessedGetUpdated(t *testing.T) {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %s", err)
	}
	defer runtime.Cleanup()

	client := client()
	sm := NewSignatureManager().(*signatureManager)
	sm.Signature(runtime.NewContext(), name, client)
	// make last accessed be in the past to account for the fact that
	// two consecutive calls to time.Now() can return identical values.
	sm.cache[name].lastAccessed = sm.cache[name].lastAccessed.Add(-ttl / 2)
	prevAccess := sm.cache[name].lastAccessed

	// access again
	sm.Signature(runtime.NewContext(), name, client)
	newAccess := sm.cache[name].lastAccessed

	if !newAccess.After(prevAccess) {
		t.Errorf("LastAccessed was not updated for cache entry")
	}
}

func TestThatTTLExpires(t *testing.T) {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("Could not initialize runtime: %s", err)
	}
	defer runtime.Cleanup()

	client := client()
	sm := NewSignatureManager().(*signatureManager)
	sm.Signature(runtime.NewContext(), name, client)

	// make last accessed go over the ttl
	sm.cache[name].lastAccessed = sm.cache[name].lastAccessed.Add(-2 * ttl)

	// make a second call
	sm.Signature(runtime.NewContext(), name, client)

	// expect number of calls to Signature method of client to be 2 since cache should have expired
	if client.TimesCalled("__Signature") != 2 {
		t.Errorf("Cache was still used but TTL had passed. It should have been fetched again")
	}
}

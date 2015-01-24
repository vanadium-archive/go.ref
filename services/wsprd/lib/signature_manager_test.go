package lib

import (
	"reflect"
	"testing"

	_ "v.io/core/veyron/profiles/fake"
	"v.io/core/veyron/runtimes/fake"
	mocks_ipc "v.io/core/veyron/runtimes/google/testing/mocks/ipc"
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vdl/vdlroot/src/signature"
)

const (
	name = "/veyron/name"
)

func initContext(t *testing.T) (*context.T, *mocks_ipc.SimpleMockClient, veyron2.Shutdown) {
	ctx, shutdown := veyron2.Init()
	initialSig := []signature.Interface{
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
	client := mocks_ipc.NewSimpleClient(
		map[string][]interface{}{
			"__Signature": []interface{}{initialSig, nil},
		},
	)
	ctx = fake.SetClient(ctx, client)
	return ctx, client, shutdown
}

func TestFetching(t *testing.T) {
	ctx, _, shutdown := initContext(t)
	defer shutdown()

	sm := NewSignatureManager()
	got, err := sm.Signature(ctx, name)
	if err != nil {
		t.Errorf(`Did not expect an error but got %v`, err)
		return
	}

	want := []signature.Interface{
		{
			Methods: []signature.Method{
				{
					Name:    "Method1",
					InArgs:  []signature.Arg{{Type: vdl.StringType}},
					OutArgs: []signature.Arg{},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf(`Signature got %v, want %v`, got, want)
	}
}

func TestThatCachedAfterFetching(t *testing.T) {
	ctx, _, shutdown := initContext(t)
	defer shutdown()

	sm := NewSignatureManager().(*signatureManager)
	sig, _ := sm.Signature(ctx, name)
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
	ctx, client, shutdown := initContext(t)
	defer shutdown()

	// call twice
	sm := NewSignatureManager()
	sm.Signature(ctx, name)
	sm.Signature(ctx, name)

	// expect number of calls to Signature method of client to still be 1 since cache
	// should have been used despite the second call
	if client.TimesCalled("__Signature") != 1 {
		t.Errorf("Signature cache was not used for the second call")
	}
}

func TestThatLastAccessedGetUpdated(t *testing.T) {
	ctx, _, shutdown := initContext(t)
	defer shutdown()

	sm := NewSignatureManager().(*signatureManager)
	sm.Signature(ctx, name)
	// make last accessed be in the past to account for the fact that
	// two consecutive calls to time.Now() can return identical values.
	sm.cache[name].lastAccessed = sm.cache[name].lastAccessed.Add(-ttl / 2)
	prevAccess := sm.cache[name].lastAccessed

	// access again
	sm.Signature(ctx, name)
	newAccess := sm.cache[name].lastAccessed

	if !newAccess.After(prevAccess) {
		t.Errorf("LastAccessed was not updated for cache entry")
	}
}

func TestThatTTLExpires(t *testing.T) {
	ctx, client, shutdown := initContext(t)
	defer shutdown()

	sm := NewSignatureManager().(*signatureManager)
	sm.Signature(ctx, name)

	// make last accessed go over the ttl
	sm.cache[name].lastAccessed = sm.cache[name].lastAccessed.Add(-2 * ttl)

	// make a second call
	sm.Signature(ctx, name)

	// expect number of calls to Signature method of client to be 2 since cache should have expired
	if client.TimesCalled("__Signature") != 2 {
		t.Errorf("Cache was still used but TTL had passed. It should have been fetched again")
	}
}

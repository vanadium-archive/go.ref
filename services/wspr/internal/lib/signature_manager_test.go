// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"reflect"
	"sync"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"
	"v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/test"
)

const (
	name = "/vanadium/name"
)

func initRuntime(t *testing.T) (*context.T, clientWithTimesCalled, v23.Shutdown) {
	ctx, shutdown := test.V23InitSimple()
	initialSig := []signature.Interface{
		{
			Methods: []signature.Method{
				{
					Name:   "Method1",
					InArgs: []signature.Arg{{Type: vdl.StringType}},
				},
			},
		},
	}
	client := newSimpleClient(
		map[string][]interface{}{
			"__Signature": []interface{}{initialSig},
		},
	)
	ctx = fake.SetClientFactory(ctx, func(ctx *context.T, opts ...rpc.ClientOpt) rpc.Client {
		return client
	})
	return ctx, client, shutdown
}

func TestFetching(t *testing.T) {
	ctx, _, shutdown := initRuntime(t)
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
					Name:   "Method1",
					InArgs: []signature.Arg{{Type: vdl.StringType}},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf(`Signature got %v, want %v`, got, want)
	}
}

func TestThatCachedAfterFetching(t *testing.T) {
	ctx, _, shutdown := initRuntime(t)
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
	ctx, client, shutdown := initRuntime(t)
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
	ctx, _, shutdown := initRuntime(t)
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
	ctx, client, shutdown := initRuntime(t)
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

func TestConcurrency(t *testing.T) {
	ctx, client, shutdown := initRuntime(t)
	defer shutdown()

	sm := NewSignatureManager().(*signatureManager)
	var wg sync.WaitGroup

	wg.Add(2)
	// Even though the signature calls return immediately in the fake client,
	// running this with the race detector should find races if the locking is done
	// poorly.
	go func() {
		sm.Signature(ctx, name)
		wg.Done()
	}()

	go func() {
		sm.Signature(ctx, name)
		wg.Done()
	}()

	wg.Wait()
	// expect number of calls to Signature method of client to be 1 since the second call should
	// wait until the first finished.
	if client.TimesCalled("__Signature") != 1 {
		t.Errorf("__Signature should only be called once.")
	}
}

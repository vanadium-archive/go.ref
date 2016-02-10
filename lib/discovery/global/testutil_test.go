// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"

	idiscovery "v.io/x/ref/lib/discovery"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/testutil"
)

// withPrincipal creates a new principal with an extension of the default
// blessing of the principal in the context.
func withPrincipal(ctx *context.T, extension string) (*context.T, error) {
	idp := testutil.IDProviderFromPrincipal(v23.GetPrincipal(ctx))
	newctx, err := v23.WithPrincipal(ctx, testutil.NewPrincipal())
	if err != nil {
		return ctx, err
	}
	if err := idp.Bless(v23.GetPrincipal(newctx), extension); err != nil {
		return ctx, err
	}
	return newctx, nil
}

func advertise(ctx *context.T, gr discovery.T, visibility []security.BlessingPattern, services ...*discovery.Service) (func(), error) {
	var wg sync.WaitGroup
	tr := idiscovery.NewTrigger()
	ctx, cancel := context.WithCancel(ctx)
	for _, service := range services {
		wg.Add(1)
		done, err := gr.Advertise(ctx, service, visibility)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("Advertise failed: %v", err)
		}
		tr.Add(wg.Done, done)
	}
	stop := func() {
		cancel()
		wg.Wait()
	}
	return stop, nil
}

func startScan(ctx *context.T, ds discovery.T, instanceId string) (<-chan discovery.Update, func(), error) {
	var query string
	if len(instanceId) > 0 {
		query = `k="` + instanceId + `"`
	}

	ctx, stop := context.WithCancel(ctx)
	scan, err := ds.Scan(ctx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("Scan failed: %v", err)
	}
	return scan, stop, err
}

func scan(ctx *context.T, ds discovery.T, instanceId string) ([]discovery.Update, error) {
	scan, stop, err := startScan(ctx, ds, instanceId)
	if err != nil {
		return nil, err
	}
	defer stop()

	var updates []discovery.Update
	for {
		select {
		case update := <-scan:
			updates = append(updates, update)
		case <-time.After(100 * time.Millisecond):
			return updates, nil
		}
	}
}

func scanAndMatch(ctx *context.T, ds discovery.T, instanceId string, wants ...discovery.Service) error {
	const timeout = 10 * time.Second

	var updates []discovery.Update
	for now := time.Now(); time.Since(now) < timeout; {
		runtime.Gosched()

		var err error
		updates, err = scan(ctx, ds, instanceId)
		if err != nil {
			return err
		}
		if matchFound(updates, wants...) {
			return nil
		}
	}
	return fmt.Errorf("Match failed; got %v, but wanted %v", updates, wants)
}

func match(updates []discovery.Update, lost bool, wants ...discovery.Service) bool {
	for _, want := range wants {
		matched := false
		for i, update := range updates {
			switch u := update.(type) {
			case discovery.UpdateFound:
				matched = !lost && serviceEqual(u.Value.Service, want)
			case discovery.UpdateLost:
				matched = lost && serviceEqual(u.Value.Service, want)
			}
			if matched {
				updates = append(updates[:i], updates[i+1:]...)
				break
			}
		}
		if !matched {
			return false
		}
	}
	return len(updates) == 0
}

func matchFound(updates []discovery.Update, wants ...discovery.Service) bool {
	return match(updates, false, wants...)
}

func matchLost(updates []discovery.Update, wants ...discovery.Service) bool {
	return match(updates, true, wants...)
}

func serviceEqual(a, b discovery.Service) bool {
	sort.Strings(a.Addrs)
	sort.Strings(b.Addrs)
	return reflect.DeepEqual(a, b)
}

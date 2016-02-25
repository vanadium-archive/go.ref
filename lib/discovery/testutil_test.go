// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery_test

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/security"

	idiscovery "v.io/x/ref/lib/discovery"
	"v.io/x/ref/lib/security/bcrypter"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/testutil"
)

func advertise(ctx *context.T, d discovery.T, visibility []security.BlessingPattern, services ...*discovery.Service) (func(), error) {
	var wg sync.WaitGroup
	tr := idiscovery.NewTrigger()
	ctx, cancel := context.WithCancel(ctx)
	for _, service := range services {
		wg.Add(1)
		done, err := d.Advertise(ctx, service, visibility)
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

func startScan(ctx *context.T, d discovery.T, interfaceName string) (<-chan discovery.Update, func(), error) {
	var query string
	if len(interfaceName) > 0 {
		query = `v.InterfaceName="` + interfaceName + `"`
	}

	ctx, stop := context.WithCancel(ctx)
	scan, err := d.Scan(ctx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("Scan failed: %v", err)
	}
	return scan, stop, err
}

func scan(ctx *context.T, d discovery.T, interfaceName string) ([]discovery.Update, error) {
	scan, stop, err := startScan(ctx, d, interfaceName)
	if err != nil {
		return nil, err
	}
	defer stop()

	var updates []discovery.Update
	for {
		select {
		case update := <-scan:
			updates = append(updates, update)
		case <-time.After(800 * time.Millisecond):
			return updates, nil
		}
	}
}

func scanAndMatch(ctx *context.T, d discovery.T, interfaceName string, wants ...discovery.Service) error {
	const timeout = 3 * time.Second

	var updates []discovery.Update
	for now := time.Now(); time.Since(now) < timeout; {
		runtime.Gosched()

		var err error
		updates, err = scan(ctx, d, interfaceName)
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
				matched = !lost && reflect.DeepEqual(u.Value.Service, want)
			case discovery.UpdateLost:
				matched = lost && reflect.DeepEqual(u.Value.Service, want)
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

func withDerivedCrypter(ctx *context.T, root *bcrypter.Root, blessing string) (*context.T, error) {
	ctx, err := v23.WithPrincipal(ctx, testutil.NewPrincipal(blessing))
	if err != nil {
		return nil, err
	}
	key, err := root.Extract(ctx, blessing)
	if err != nil {
		return nil, err
	}
	crypter := bcrypter.NewCrypter()
	ctx = bcrypter.WithCrypter(ctx, crypter)
	if err := crypter.AddKey(ctx, key); err != nil {
		return nil, err
	}
	return ctx, nil
}

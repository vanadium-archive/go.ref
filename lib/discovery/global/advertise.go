// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

const (
	mountTTL      = 120 * time.Second
	mountTTLSlack = 20 * time.Second
)

var (
	errAlreadyBeingAdvertised = verror.Register(pkgPath+".errAlreadyBeingAdvertised", verror.NoRetry, "{1:}{2:} already being advertised")
	errInvalidService         = verror.Register(pkgPath+"errInvalidService", verror.NoRetry, "{1:}{2:} service not valid{:_}")
)

func (d *gdiscovery) Advertise(ctx *context.T, service *discovery.Service, visibility []security.BlessingPattern) (<-chan struct{}, error) {
	if len(service.InstanceId) == 0 {
		service.InstanceId = newInstanceId()
	}
	if err := validateService(service); err != nil {
		return nil, verror.New(errInvalidService, ctx, err)
	}
	if len(visibility) == 0 {
		visibility = []security.BlessingPattern{security.AllPrincipals}
	}

	principal := v23.GetPrincipal(ctx)
	self := security.DefaultBlessingPatterns(principal)
	perms := access.Permissions{
		string(access.Admin): access.AccessList{In: self},
		string(access.Read):  access.AccessList{In: visibility},
	}

	name := service.InstanceId
	if !d.addAd(name) {
		return nil, verror.New(errAlreadyBeingAdvertised, ctx)
	}

	// TODO(jhahn): There is no atomic way to check and reserve the name under mounttable.
	// For example, the name can be overwritten by other applications of the same owner.
	// But this would be OK for now.
	if err := d.ns.SetPermissions(ctx, name, perms, "", naming.IsLeaf(true)); err != nil {
		d.removeAd(name)
		return nil, err
	}

	// TODO(jhahn): We're using one goroutine per advertisement, but we can do
	// better like have one goroutine that takes care of all advertisements.
	// But this is OK for now as an experiment.
	addrs := service.Addrs
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer d.removeAd(name)
		// We need a context that is detached from the deadlines and cancellation
		// of ctx since we have to unmount after ctx is canceled.
		rctx, _ := context.WithRootCancel(ctx)
		defer unmount(rctx, d.ns, name)

		for {
			mount(ctx, d.ns, name, addrs)

			select {
			case <-d.clock.After(mountTTL):
			case <-ctx.Done():
				return
			}
		}
	}()
	return done, nil
}

func (d *gdiscovery) addAd(id string) bool {
	d.mu.Lock()
	if _, exist := d.ads[id]; exist {
		d.mu.Unlock()
		return false
	}
	d.ads[id] = struct{}{}
	d.mu.Unlock()
	return true
}

func (d *gdiscovery) removeAd(id string) {
	d.mu.Lock()
	delete(d.ads, id)
	d.mu.Unlock()
}

func mount(ctx *context.T, ns namespace.T, name string, addrs []string) {
	const ttl = mountTTL + mountTTLSlack

	for _, addr := range addrs {
		if err := ns.Mount(ctx, name, addr, ttl); err != nil {
			ctx.Errorf("mount(%q, %q) failed: %v", name, addr, err)
		}
	}
}

func unmount(ctx *context.T, ns namespace.T, name string) {
	if err := ns.Delete(ctx, name, true); err != nil {
		ctx.Infof("unmount(%q) failed: %v", name, err)
	}
}

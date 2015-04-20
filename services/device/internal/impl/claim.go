// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"crypto/subtle"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/internal/pathperms"
)

// claimable implements the device.Claimable RPC interface and the
// rpc.Dispatcher and security.Authorizer to serve it.
//
// It allows the Claim RPC to be successfully invoked exactly once.
type claimable struct {
	token      string
	permsStore *pathperms.PathStore
	permsDir   string
	notify     chan struct{} // GUARDED_BY(mu)

	// Lock used to ensure that a successful claim can happen at most once.
	// This is done by allowing only a single goroutine to execute the
	// meaty parts of Claim at a time.
	mu sync.Mutex
}

func (c *claimable) Claim(ctx *context.T, call rpc.ServerCall, pairingToken string) error {
	// Verify that the claimer pairing tokens match that of the device manager.
	if subtle.ConstantTimeCompare([]byte(pairingToken), []byte(c.token)) != 1 {
		return verror.New(ErrInvalidPairingToken, ctx)
	}
	var (
		granted   = call.GrantedBlessings() // blessings granted by the claimant
		principal = v23.GetPrincipal(ctx)
		store     = principal.BlessingStore()
	)
	if granted.IsZero() {
		return verror.New(ErrInvalidBlessing, ctx)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.notify == nil {
		// Device has already been claimed (by a concurrent
		// RPC perhaps), it cannot be reclaimed
		return verror.New(ErrDeviceAlreadyClaimed, ctx)
	}
	// TODO(ashankar): If the claim fails, would make sense
	// to remove from roots as well.
	if err := principal.AddToRoots(granted); err != nil {
		return verror.New(ErrInvalidBlessing, ctx)
	}
	if _, err := store.Set(granted, security.AllPrincipals); err != nil {
		return verror.New(ErrInvalidBlessing, ctx, err)
	}
	if err := store.SetDefault(granted); err != nil {
		return verror.New(ErrInvalidBlessing, ctx, err)
	}

	// Create Permissions with all the granted blessings (which are now the default blessings)
	// (irrespective of caveats).
	patterns := security.DefaultBlessingPatterns(principal)
	if len(patterns) == 0 {
		return verror.New(ErrInvalidBlessing, ctx)
	}

	// Create Permissions that allow principals with the caller's blessings to
	// administer and use the device.
	perms := make(access.Permissions)
	for _, bp := range patterns {
		// TODO(caprita,ataly,ashankar): Do we really need the
		// NonExtendable restriction below?
		patterns := bp.MakeNonExtendable().PrefixPatterns()
		for _, p := range patterns {
			for _, tag := range access.AllTypicalTags() {
				perms.Add(p, string(tag))
			}
		}
	}
	if err := c.permsStore.Set(c.permsDir, perms, ""); err != nil {
		return verror.New(ErrOperationFailed, ctx)
	}
	vlog.Infof("Device claimed and Permissions set to: %v", perms)
	close(c.notify)
	c.notify = nil
	return nil
}

// TODO(ashankar): Remove this and use Serve instead of ServeDispatcher to setup
// the Claiming service. Shouldn't need the "device" suffix.
func (c *claimable) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if suffix != "" && suffix != "device" {
		return nil, nil, verror.New(ErrUnclaimedDevice, nil)
	}
	return c, c, nil
}

func (c *claimable) Authorize(*context.T, security.Call) error {
	// Claim is open to all. The Claim method implementation
	// allows at most one successful call.
	return nil
}

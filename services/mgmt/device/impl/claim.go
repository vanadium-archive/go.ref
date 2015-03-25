// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"crypto/subtle"
	"sync"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/mgmt/lib/acls"
)

// claimable implements the device.Claimable RPC interface and the
// rpc.Dispatcher and security.Authorizer to serve it.
//
// It allows the Claim RPC to be successfully invoked exactly once.
type claimable struct {
	token    string
	aclstore *acls.PathStore
	aclDir   string
	notify   chan struct{} // GUARDED_BY(mu)

	// Lock used to ensure that a successful claim can happen at most once.
	// This is done by allowing only a single goroutine to execute the
	// meaty parts of Claim at a time.
	mu sync.Mutex
}

func (c *claimable) Claim(call rpc.ServerCall, pairingToken string) error {
	// Verify that the claimer pairing tokens match that of the device manager.
	if subtle.ConstantTimeCompare([]byte(pairingToken), []byte(c.token)) != 1 {
		return verror.New(ErrInvalidPairingToken, call.Context())
	}
	var (
		granted   = call.GrantedBlessings() // blessings granted by the claimant
		principal = call.LocalPrincipal()
		store     = principal.BlessingStore()
	)
	if granted.IsZero() {
		return verror.New(ErrInvalidBlessing, call.Context())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.notify == nil {
		// Device has already been claimed (by a concurrent
		// RPC perhaps), it cannot be reclaimed
		return verror.New(ErrDeviceAlreadyClaimed, call.Context())
	}
	// TODO(ashankar): If the claim fails, would make sense
	// to remove from roots as well.
	if err := principal.AddToRoots(granted); err != nil {
		return verror.New(ErrInvalidBlessing, call.Context())
	}
	if _, err := store.Set(granted, security.AllPrincipals); err != nil {
		return verror.New(ErrInvalidBlessing, call.Context(), err)
	}
	if err := store.SetDefault(granted); err != nil {
		return verror.New(ErrInvalidBlessing, call.Context(), err)
	}

	// Create an AccessList with all the granted blessings (which are now the default blessings)
	// (irrespective of caveats).
	patterns := security.DefaultBlessingPatterns(principal)
	if len(patterns) == 0 {
		return verror.New(ErrInvalidBlessing, call.Context())
	}

	// Create AccessLists that allow principals with the caller's blessings to
	// administer and use the device.
	acl := make(access.Permissions)
	for _, bp := range patterns {
		// TODO(caprita,ataly,ashankar): Do we really need the
		// NonExtendable restriction below?
		patterns := bp.MakeNonExtendable().PrefixPatterns()
		for _, p := range patterns {
			for _, tag := range access.AllTypicalTags() {
				acl.Add(p, string(tag))
			}
		}
	}
	if err := c.aclstore.Set(c.aclDir, acl, ""); err != nil {
		return verror.New(ErrOperationFailed, call.Context())
	}
	vlog.Infof("Device claimed and AccessLists set to: %v", acl)
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

func (c *claimable) Authorize(*context.T) error {
	// Claim is open to all. The Claim method implementation
	// allows at most one successful call.
	return nil
}

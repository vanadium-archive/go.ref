package impl

import (
	"crypto/subtle"
	"sync"

	"v.io/core/veyron/services/mgmt/lib/acls"
	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

// claimable implements the device.Claimable RPC interface and the
// ipc.Dispatcher and security.Authorizer to serve it.
//
// It allows the Claim RPC to be successfully invoked exactly once.
type claimable struct {
	token  string
	locks  *acls.Locks
	aclDir string
	notify chan struct{} // GUARDED_BY(mu)

	// Lock used to ensure that a successful claim can happen at most once.
	// This is done by allowing only a single goroutine to execute the
	// meaty parts of Claim at a time.
	mu sync.Mutex
}

func (c *claimable) Claim(ctx ipc.ServerContext, pairingToken string) error {
	// Verify that the claimer pairing tokens match that of the device manager.
	if subtle.ConstantTimeCompare([]byte(pairingToken), []byte(c.token)) != 1 {
		return verror.New(ErrInvalidPairingToken, ctx.Context())
	}
	var (
		granted   = ctx.Blessings() // blessings granted by the claimant
		principal = ctx.LocalPrincipal()
		store     = principal.BlessingStore()
	)
	if granted.IsZero() {
		return verror.New(ErrInvalidBlessing, ctx.Context())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.notify == nil {
		// Device has already been claimed (by a concurrent
		// RPC perhaps), it cannot be reclaimed
		return verror.New(ErrDeviceAlreadyClaimed, ctx.Context())
	}
	// TODO(ashankar): If the claim fails, would make sense
	// to remove from roots as well.
	if err := principal.AddToRoots(granted); err != nil {
		return verror.New(ErrInvalidBlessing, ctx.Context())
	}
	// Create an ACL with all the granted blessings
	// (irrespective of caveats).
	blessings := principal.BlessingsInfo(granted)
	if len(blessings) == 0 {
		return verror.New(ErrInvalidBlessing, ctx.Context())
	}
	if _, err := store.Set(granted, security.AllPrincipals); err != nil {
		return verror.New(ErrInvalidBlessing, ctx.Context(), err)
	}
	if err := store.SetDefault(granted); err != nil {
		return verror.New(ErrInvalidBlessing, ctx.Context(), err)
	}
	// Create ACLs that allow principals with the caller's blessings to
	// administer and use the device.
	acl := make(access.TaggedACLMap)
	for b, _ := range blessings {
		// TODO(caprita,ataly,ashankar): Do we really need the
		// NonExtendable restriction below?
		patterns := security.BlessingPattern(b).MakeNonExtendable().PrefixPatterns()
		for _, p := range patterns {
			for _, tag := range access.AllTypicalTags() {
				acl.Add(p, string(tag))
			}
		}
	}
	if err := c.locks.SetPathACL(principal, c.aclDir, acl, ""); err != nil {
		return verror.New(ErrOperationFailed, ctx.Context())
	}
	vlog.Infof("Device claimed and ACLs set to: %v", acl)
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

func (c *claimable) Authorize(security.Context) error {
	// Claim is open to all. The Claim method implementation
	// allows at most one successful call.
	return nil
}

package ipc

import (
	"fmt"

	"veyron.io/veyron/veyron2/security"
)

// defaultAuthorizer implements a security.Authorizer with an authorization
// policy that requires one end of the RPC to have a blessing that makes it a
// delegate of the other.
type defaultAuthorizer struct{}

func (defaultAuthorizer) Authorize(ctx security.Context) error {
	var (
		local  = ctx.LocalBlessings().ForContext(ctx)
		remote = ctx.RemoteBlessings().ForContext(ctx)
	)
	// Authorize if any element in local is a "delegate of" (i.e., has been
	// blessed by) any element in remote, OR vice-versa.
	for _, l := range local {
		if security.BlessingPattern(l).MatchedBy(remote...) {
			// l is a delegate of an element in remote.
			return nil
		}
	}
	for _, r := range remote {
		if security.BlessingPattern(r).MatchedBy(local...) {
			// r is a delegate of an element in local.
			return nil
		}
	}
	return fmt.Errorf("policy disallows %v", remote)
}

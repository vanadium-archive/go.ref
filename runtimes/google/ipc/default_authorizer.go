package ipc

import (
	"fmt"

	"v.io/veyron/veyron2/security"
)

// defaultAuthorizer implements a security.Authorizer with an authorization
// policy that requires one end of the RPC to have a blessing that makes it a
// delegate of the other.
type defaultAuthorizer struct{}

func (defaultAuthorizer) Authorize(ctx security.Context) error {
	var (
		localForContext  = ctx.LocalBlessings().ForContext(ctx)
		remote           = ctx.RemoteBlessings()
		remoteForContext = remote.ForContext(ctx)
	)
	// Authorize if any element in localForContext is a "delegate of" (i.e., has been
	// blessed by) any element in remote, OR vice-versa.
	for _, l := range localForContext {
		if security.BlessingPattern(l).MatchedBy(remoteForContext...) {
			// l is a delegate of an element in remote.
			return nil
		}
	}
	for _, r := range remoteForContext {
		if security.BlessingPattern(r).MatchedBy(localForContext...) {
			// r is a delegate of an element in localForContext.
			return nil
		}
	}

	// TODO(ataly, ashankar, caprita): Below we implicitly invoke the String() on
	// remote blessings in order to construct thre error messsage. This is somewhat
	// breaking encapsulation as the String() method is hidden from the public API
	// and is only meant for debugging purposes. Should we make the 'String' method
	// public?
	return fmt.Errorf("all valid blessings for this request: %v (out of %v) are disallowed by the policy", remoteForContext, remote)
}

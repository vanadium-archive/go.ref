package ipc

import (
	"v.io/v23/security"
)

// defaultAuthorizer implements a security.Authorizer with an authorization
// policy that requires one end of the RPC to have a blessing that makes it a
// delegate of the other.
type defaultAuthorizer struct{}

func (defaultAuthorizer) Authorize(ctx security.Context) error {
	var (
		localForContext, localErr   = ctx.LocalBlessings().ForContext(ctx)
		remote                      = ctx.RemoteBlessings()
		remoteForContext, remoteErr = remote.ForContext(ctx)
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

	return NewErrInvalidBlessings(nil, remoteForContext, remoteErr, localForContext, localErr)
}

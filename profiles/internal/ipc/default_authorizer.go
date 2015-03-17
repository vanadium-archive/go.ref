package ipc

import (
	"v.io/v23/context"
	"v.io/v23/security"
)

// defaultAuthorizer implements a security.Authorizer with an authorization
// policy that requires one end of the RPC to have a blessing that makes it a
// delegate of the other.
type defaultAuthorizer struct{}

func (defaultAuthorizer) Authorize(ctx *context.T) error {
	var (
		localNames, localErr   = security.BlessingNames(ctx, security.CallSideLocal)
		remoteNames, remoteErr = security.BlessingNames(ctx, security.CallSideRemote)
	)
	// Authorize if any element in localNames is a "delegate of" (i.e., has been
	// blessed by) any element in remoteNames, OR vice-versa.
	for _, l := range localNames {
		if security.BlessingPattern(l).MatchedBy(remoteNames...) {
			// l is a delegate of an element in remote.
			return nil
		}
	}
	for _, r := range remoteNames {
		if security.BlessingPattern(r).MatchedBy(localNames...) {
			// r is a delegate of an element in localNames.
			return nil
		}
	}

	return NewErrInvalidBlessings(nil, remoteNames, remoteErr, localNames, localErr)
}

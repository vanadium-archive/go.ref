package ipc

import (
	"v.io/v23/security"
)

// defaultAuthorizer implements a security.Authorizer with an authorization
// policy that requires one end of the RPC to have a blessing that makes it a
// delegate of the other.
type defaultAuthorizer struct{}

func (defaultAuthorizer) Authorize(call security.Call) error {
	var (
		localForCall, localErr   = call.LocalBlessings().ForCall(call)
		remote                   = call.RemoteBlessings()
		remoteForCall, remoteErr = remote.ForCall(call)
	)
	// Authorize if any element in localForCall is a "delegate of" (i.e., has been
	// blessed by) any element in remote, OR vice-versa.
	for _, l := range localForCall {
		if security.BlessingPattern(l).MatchedBy(remoteForCall...) {
			// l is a delegate of an element in remote.
			return nil
		}
	}
	for _, r := range remoteForCall {
		if security.BlessingPattern(r).MatchedBy(localForCall...) {
			// r is a delegate of an element in localForCall.
			return nil
		}
	}

	return NewErrInvalidBlessings(nil, remoteForCall, remoteErr, localForCall, localErr)
}

package ipc

import (
	"errors"
	"reflect"

	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/v23/verror"
)

// TODO(ribrdb): Flip this to true once everything is updated and also update
// the server authorizer tests.
const enableSecureServerAuth = false

var (
	errNoBlessings = verror.Register(pkgPath+".noBlessings", verror.NoRetry, "server has not presented any blessings")

	errAuthNoPatternMatch = verror.Register(pkgPath+".authNoPatternMatch",
		verror.NoRetry, "server blessings {3} do not match pattern {4}{:5}")

	errAuthServerNotAllowed = verror.Register(pkgPath+".authServerNotAllowed",
		verror.NoRetry, "server blesssings {3} do not match any allowed server patterns {4}{:5}")

	errAuthServerKeyNotAllowed = verror.Register(pkgPath+".authServerKeyNotAllowed",
		verror.NoRetry, "remote public key {3} not matched by server key {4}")
)

// serverAuthorizer implements security.Authorizer.
type serverAuthorizer struct {
	patternsFromNameResolution []security.BlessingPattern
	allowedServerPolicies      [][]security.BlessingPattern
	serverPublicKey            security.PublicKey
}

// newServerAuthorizer returns a security.Authorizer for authorizing the server
// during a flow. The authorization policy is based on enforcing any server
// patterns obtained by resolving the server's name, and any server authorization
// options supplied to the call that initiated the flow.
//
// This method assumes that canCreateServerAuthorizer(opts) is nil.
func newServerAuthorizer(ctx *context.T, patternsFromNameResolution []security.BlessingPattern, opts ...ipc.CallOpt) security.Authorizer {
	auth := &serverAuthorizer{
		patternsFromNameResolution: patternsFromNameResolution,
	}
	for _, o := range opts {
		// TODO(ataly, ashankar): Consider creating an authorizer for each of the
		// options below and then take the intersection of the authorizers.
		switch v := o.(type) {
		case options.ServerPublicKey:
			auth.serverPublicKey = v.PublicKey
		case options.AllowedServersPolicy:
			auth.allowedServerPolicies = append(auth.allowedServerPolicies, v)
		case options.SkipResolveAuthorization:
			auth.patternsFromNameResolution = []security.BlessingPattern{security.AllPrincipals}
		}
	}
	return auth
}

func (a *serverAuthorizer) Authorize(ctx security.Context) error {
	if ctx.RemoteBlessings().IsZero() {
		return verror.New(errNoBlessings, ctx.Context())
	}
	serverBlessings, rejectedBlessings := ctx.RemoteBlessings().ForContext(ctx)

	if !matchedBy(a.patternsFromNameResolution, serverBlessings) {
		return verror.New(errAuthNoPatternMatch, ctx.Context(), serverBlessings, a.patternsFromNameResolution, rejectedBlessings)
	} else if enableSecureServerAuth {
		// No server patterns were obtained while resolving the name, authorize
		// the server using the default authorization policy.
		if err := (defaultAuthorizer{}).Authorize(ctx); err != nil {
			return verror.New(errDefaultAuthDenied, ctx.Context(), serverBlessings)
		}
	}

	for _, patterns := range a.allowedServerPolicies {
		if !matchedBy(patterns, serverBlessings) {
			return verror.New(errAuthServerNotAllowed, ctx.Context(), serverBlessings, patterns, rejectedBlessings)
		}
	}

	if remoteKey, key := ctx.RemoteBlessings().PublicKey(), a.serverPublicKey; key != nil && !reflect.DeepEqual(remoteKey, key) {
		return verror.New(errAuthServerKeyNotAllowed, ctx.Context(), remoteKey, key)
	}

	return nil
}

func matchedBy(patterns []security.BlessingPattern, blessings []string) bool {
	if patterns == nil {
		return true
	}
	for _, p := range patterns {
		if p.MatchedBy(blessings...) {
			return true
		}
	}
	return false
}

func canCreateServerAuthorizer(opts []ipc.CallOpt) error {
	var pkey security.PublicKey
	for _, o := range opts {
		switch v := o.(type) {
		case options.ServerPublicKey:
			if pkey != nil && !reflect.DeepEqual(pkey, v.PublicKey) {
				return errors.New("multiple ServerPublicKey options supplied to call, at most one is allowed")
			}
			pkey = v.PublicKey
		}
	}
	return nil
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"reflect"

	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
)

// TODO(ribrdb): Flip this to true once everything is updated and also update
// the server authorizer tests.
const enableSecureServerAuth = false

var (
	errNoBlessings = verror.Register(pkgPath+".noBlessings", verror.NoRetry, "server has not presented any blessings")

	errAuthPossibleManInTheMiddle = verror.Register(pkgPath+".authPossibleManInTheMiddle",
		verror.NoRetry, "server blessings {3} do not match expectations set by endpoint {4}, possible man-in-the-middle or the server blessings are not accepted by the client? (endpoint: {5}, rejected blessings: {6})")

	errAuthServerNotAllowed = verror.Register(pkgPath+".authServerNotAllowed",
		verror.NoRetry, "server blessings {3} do not match any allowed server patterns {4}{:5}")

	errAuthServerKeyNotAllowed = verror.Register(pkgPath+".authServerKeyNotAllowed",
		verror.NoRetry, "remote public key {3} not matched by server key {4}")

	errMultiplePublicKeys = verror.Register(pkgPath+".multiplePublicKeyOptions", verror.NoRetry, "multiple ServerPublicKey options supplied to call, at most one is allowed")
)

// serverAuthorizer implements security.Authorizer.
type serverAuthorizer struct {
	allowedServerPolicies     [][]security.BlessingPattern
	serverPublicKey           security.PublicKey
	ignoreBlessingsInEndpoint bool
}

// newServerAuthorizer returns a security.Authorizer for authorizing the server
// during a flow. The authorization policy is based on options supplied to the
// call that initiated the flow. Additionally, if pattern is non-empty then
// the server will be authorized only if it presents at least one blessing
// that matches pattern.
//
// This method assumes that canCreateServerAuthorizer(opts) is nil.
func newServerAuthorizer(pattern security.BlessingPattern, opts ...rpc.CallOpt) security.Authorizer {
	auth := &serverAuthorizer{}
	for _, o := range opts {
		// TODO(ataly, ashankar): Consider creating an authorizer for each of the
		// options below and then take the intersection of the authorizers.
		switch v := o.(type) {
		case options.ServerPublicKey:
			auth.serverPublicKey = v.PublicKey
		case options.AllowedServersPolicy:
			auth.allowedServerPolicies = append(auth.allowedServerPolicies, v)
		case options.SkipServerEndpointAuthorization:
			auth.ignoreBlessingsInEndpoint = true
		}
	}
	if len(pattern) > 0 {
		auth.allowedServerPolicies = append(auth.allowedServerPolicies, []security.BlessingPattern{pattern})
	}
	return auth
}

func (a *serverAuthorizer) Authorize(ctx *context.T) error {
	call := security.GetCall(ctx)
	if call.RemoteBlessings().IsZero() {
		return verror.New(errNoBlessings, ctx)
	}
	serverBlessings, rejectedBlessings := security.RemoteBlessingNames(ctx)

	if epb := call.RemoteEndpoint().BlessingNames(); len(epb) > 0 && !a.ignoreBlessingsInEndpoint {
		matched := false
		for _, b := range epb {
			// TODO(ashankar,ataly): Should this be
			// security.BlessingPattern(b).MakeNonExtendable().MatchedBy()?
			// Because, without that, a delegate of the real server
			// can be a man-in-the-middle without failing
			// authorization. Is that a desirable property?
			if security.BlessingPattern(b).MatchedBy(serverBlessings...) {
				matched = true
				break
			}
		}
		if !matched {
			return verror.New(errAuthPossibleManInTheMiddle, ctx, serverBlessings, epb, call.RemoteEndpoint(), rejectedBlessings)
		}
	} else if enableSecureServerAuth && len(epb) == 0 {
		// No blessings in the endpoint to set expectations on the
		// "identity" of the server.  Use the default authorization
		// policy.
		if err := security.DefaultAuthorizer().Authorize(ctx); err != nil {
			return err
		}
	}

	for _, patterns := range a.allowedServerPolicies {
		if !matchedBy(patterns, serverBlessings) {
			return verror.New(errAuthServerNotAllowed, ctx, serverBlessings, patterns, rejectedBlessings)
		}
	}

	if remoteKey, key := call.RemoteBlessings().PublicKey(), a.serverPublicKey; key != nil && !reflect.DeepEqual(remoteKey, key) {
		return verror.New(errAuthServerKeyNotAllowed, ctx, remoteKey, key)
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

func canCreateServerAuthorizer(ctx *context.T, opts []rpc.CallOpt) error {
	var pkey security.PublicKey
	for _, o := range opts {
		switch v := o.(type) {
		case options.ServerPublicKey:
			if pkey != nil && !reflect.DeepEqual(pkey, v.PublicKey) {
				return verror.New(errMultiplePublicKeys, ctx)
			}
			pkey = v.PublicKey
		}
	}
	return nil
}

// Package caveat provides common security.Caveat implementations.
package caveat

import (
	"fmt"
	"time"

	"veyron2/security"
	"veyron2/vom"
)

// Expiry is a security.CaveatValidator that restricts the validity period of
// credentials bearing a security.Caveat constructed from this validator.
type Expiry struct {
	// TODO(ataly,ashankar): Get rid of IssueTime from this caveat.
	IssueTime  time.Time
	ExpiryTime time.Time
}

func (v *Expiry) Validate(context security.Context) error {
	now := time.Now()
	if now.Before(v.IssueTime) || now.After(v.ExpiryTime) {
		return fmt.Errorf("%#v forbids credential from being used at this time(%v)", v, now)
	}
	return nil
}

// MethodRestriction is a security.CaveatValidator that restricts the set of
// methods that are authorized via credentials bearing a security.Caveat
// constructed from this validator.
// An empty set indicates that no methods can be invoked.
type MethodRestriction []string

func (v MethodRestriction) Validate(ctx security.Context) error {
	// If the context has an empty Method then the caveat validates.
	if ctx.Method() == "" {
		return nil
	}
	for _, m := range v {
		if m == ctx.Method() {
			return nil
		}
	}
	return fmt.Errorf("%#v forbids invocation of method %s", v, ctx.Method())
}

// PeerBlessings is a security.CaveatValidator that restricts a credential
// bearing a security.Caveat constructed from this validator to be used for
// communicating with a limited set of peers - those who have blessings matching
// one of the provided security.BlessingPatterns.
// An empty set indicates that no peers can be communicated with.
type PeerBlessings []security.BlessingPattern

func (v PeerBlessings) Validate(ctx security.Context) error {
	for _, p := range v {
		if ctx.LocalID() != nil && p.MatchedBy(ctx.LocalID().Names()...) {
			return nil
		}
	}
	return fmt.Errorf("%#v forbids RPCing with peer %s", v, ctx.LocalID())
}

// NetworkType is a security.CaveatValidator that restricts a credential bearing
// a security.Caveat constructed from this validator to be used only for communicating over
// a particular network ("tcp", "udp", "bluetooth" etc.)
type NetworkType string

func (v NetworkType) Validate(ctx security.Context) error {
	if ctx.RemoteEndpoint().Addr().Network() == string(v) {
		return nil
	}
	return fmt.Errorf("required network type %q, got %q", v, ctx.RemoteEndpoint().Addr().Network())
}

func init() {
	vom.Register(Expiry{})
	vom.Register(MethodRestriction(nil))
	vom.Register(PeerBlessings(nil))
	vom.Register(NetworkType(""))
}

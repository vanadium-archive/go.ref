// Package caveat provides common security.Caveat implementations.
package caveat

import (
	"fmt"
	"time"

	"veyron2/security"
	"veyron2/vom"
)

// UniversalCaveat takes a Caveat and returns a ServiceCaveat bound to all principals.
func UniversalCaveat(cav security.Caveat) security.ServiceCaveat {
	return security.ServiceCaveat{Service: security.AllPrincipals, Caveat: cav}
}

// Expiry is a security.Caveat that restricts the validity period of
// the credential bearing this caveat.
type Expiry struct {
	IssueTime  time.Time
	ExpiryTime time.Time
}

func (c *Expiry) Validate(context security.Context) error {
	now := time.Now()
	if now.Before(c.IssueTime) || now.After(c.ExpiryTime) {
		return fmt.Errorf("%#v forbids credential from being used at this time(%v)", c, now)
	}
	return nil
}

// MethodRestriction is a security.Caveat that restricts the set of
// methods that can be invoked by a credential bearing the caveat.
// An empty set indicates that no methods can be invoked.
type MethodRestriction []string

func (c MethodRestriction) Validate(ctx security.Context) error {
	// If the context has an empty Method then the caveat validates.
	if ctx.Method() == "" {
		return nil
	}
	for _, m := range c {
		if m == ctx.Method() {
			return nil
		}
	}
	return fmt.Errorf("%#v forbids invocation of method %s", c, ctx.Method())
}

// PeerIdentity is a security.Caveat that restricts the bearer of a credential
// with this caveat from making or receiving RPCs to a limited set of peers -
// those whose identities match one of the provided security.PrincipalPatterns.
// An empty set indicates that no peers can be communicated with.
type PeerIdentity []security.PrincipalPattern

// Validate checks that the identity of the peer is present on the set of services
// identified by the PrincipalPatterns on the caveat.
func (c PeerIdentity) Validate(ctx security.Context) error {
	for _, p := range c {
		if ctx.LocalID() != nil && p.MatchedBy(ctx.LocalID()) {
			return nil
		}
	}
	return fmt.Errorf("%#v forbids RPCing with peer %s", c, ctx.LocalID())
}

// NetworkType is a security.Caveat that restricts communication with the
// remote process to a particular network ("tcp", "udp", "bluetooth" etc.)
type NetworkType string

func (cav NetworkType) Validate(ctx security.Context) error {
	if ctx.RemoteEndpoint().Addr().Network() == string(cav) {
		return nil
	}
	return fmt.Errorf("required network type %q, got %q", cav, ctx.RemoteEndpoint().Addr().Network())
}

func init() {
	vom.Register(Expiry{})
	vom.Register(MethodRestriction(nil))
	vom.Register(PeerIdentity(nil))
	vom.Register(NetworkType(""))
}

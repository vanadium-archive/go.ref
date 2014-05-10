// Package caveat provides a collection of standard caveats that can be validated
// by the framework. All these caveats implement the security.Caveat interface.
package caveat

import (
	"fmt"
	"time"

	"veyron2/security"
	"veyron2/vom"
)

// Expiry implements security.Caveat. It specifies a restriction
// on the validity period of the credential that bears the caveat.
type Expiry struct {
	IssueTime  time.Time
	ExpiryTime time.Time
}

func (c *Expiry) String() string {
	return fmt.Sprintf("%T%+v", *c, *c)
}

// Validate checks that the current time is within the IssueTime and ExpiryTime
// specified by the caveat.
func (c *Expiry) Validate(context security.Context) error {
	now := time.Now()
	if now.Before(c.IssueTime) || now.After(c.ExpiryTime) {
		return fmt.Errorf("ExpiryCaveat %v forbids credential from being used at this time(%v)", c, now)
	}
	return nil
}

// MethodRestriction implements security.Caveat. It restricts the methods
// that can be invoked using a credential bearing the caveat to the set
// of methods specified on the caveat. An empty set indicates that no method
// can be invoked.
type MethodRestriction struct {
	Methods []string
}

func (c *MethodRestriction) String() string {
	return fmt.Sprintf("%T%+v", *c, *c)
}

// Validate checks that if a method is being invoked then its name is present on the set
// specified on the caveat.
func (c *MethodRestriction) Validate(ctx security.Context) error {
	// If the context has an empty Method then the caveat validates.
	if ctx.Method() == "" {
		return nil
	}
	for _, m := range c.Methods {
		if m == ctx.Method() {
			return nil
		}
	}
	return fmt.Errorf("%v forbids invocation of method %s", c, ctx.Method())
}

// PeerIdentity implements security.Caveat. A credential bearing this caveat
// can be used to make and receive RPCs from only those peers whose identities
// match one of the provided security.PrincipalPatterns. If no
// security.PrincipalPatterns are provided then communication with all peers is
// forbidden.
type PeerIdentity struct {
	Peers []security.PrincipalPattern
}

func (c *PeerIdentity) String() string {
	return fmt.Sprintf("%T%+v", *c, *c)
}

// Validate checks that the identity of the peer is present on the set of services
// identified by the PrincipalPatterns on the caveat.
func (c *PeerIdentity) Validate(ctx security.Context) error {
	for _, p := range c.Peers {
		if ctx.LocalID() != nil && ctx.LocalID().Match(p) {
			return nil
		}
	}
	return fmt.Errorf("%v forbids RPCing with peer %s", c, ctx.LocalID())
}

func init() {
	vom.Register(Expiry{})
	vom.Register(MethodRestriction{})
	vom.Register(PeerIdentity{})
}

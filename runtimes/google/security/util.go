package security

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"veyron/runtimes/google/security/keys"
	"veyron/runtimes/google/security/wire"
	"veyron2/security"
)

var errDeriveMismatch = errors.New("public key does not match that of deriving identity")

// TrustIdentityProviders registers the identity providers of "id" as trustworthy ones,
// i.e., any identities created by those providers will be considered trustworthy.
func TrustIdentityProviders(id security.PrivateID) {
	switch t := id.(type) {
	case *chainPrivateID:
		keys.Trust(t.publicID.rootKey, t.publicID.certificates[0].Name)
	case *treePrivateID:
		for _, p := range t.publicID.paths {
			keys.Trust(p.providerKey, p.providerName)
		}
	default:
		// Silently ignore
	}
}

// matchesPattern checks if the provided name conforms to the provided pattern.
// This function assumes pattern to be of the one of the following forms:
// - Pattern is a chained name of the form p_0/.../p_k; in this case the check
//   succeeds iff the provided name is of the form n_0/.../n_m such that m <= k
//   and for all i from 0 to m, p_i = n_i.
// - Pattern is a chained name of the form p_0/.../p_k/*; in this case the check
//   succeeds iff the provided name is of the form n_0/.../n_m such that for all i
//   from 0 to min(m, k), p_i = n_i.
func matchesPattern(name, pattern string) bool {
	patternParts := strings.Split(pattern, wire.ChainSeparator)
	patternLen := len(patternParts)
	nameParts := strings.Split(name, wire.ChainSeparator)
	nameLen := len(nameParts)

	if patternParts[patternLen-1] != security.AllPrincipals && nameLen > patternLen {
		return false
	}

	min := nameLen
	if patternParts[patternLen-1] == security.AllPrincipals && nameLen > patternLen-1 {
		min = patternLen - 1
	}

	for i := 0; i < min; i++ {
		if patternParts[i] != nameParts[i] {
			return false
		}
	}
	return true
}

func matchPrincipalPattern(name string, pattern security.PrincipalPattern) bool {
	return pattern == security.AllPrincipals || matchesPattern(name, string(pattern))
}

// ContextArgs holds the arguments for creating a new security.Context for an IPC.
type ContextArgs struct {
	// LocalID, RemoteID are the identities at the local and remote ends of a request
	// respectively.
	LocalID, RemoteID security.PublicID
	// Discharges is the set of third-party caveat discharges for the identity at the remote end
	// of the request.
	Discharges security.CaveatDischargeMap
	// Debug describes the context for debugging purposes.
	Debug string
	// The following fields must be set only for contexts created at the server receiving the IPC.
	//
	// Method is the name of the method being invoked.
	Method string
	// Name is the undispatched name for the request.
	Name string
	// Suffix is the veyron name suffix for the request.
	Suffix string
	// Label is the security label of the method being invoked.
	Label security.Label
}

// context implements security.Context. This implementation simply stores the
// method, label, suffix, and the identities of the local and remote principals
// associated with an IPC call in the context object.
type context struct {
	ContextArgs
	Debug string
}

func (c *context) String() string {
	// fmt.Sprintf("%#v", c) doesn't work because it does not expand
	// localID, remoteID etc.
	if len(c.ContextArgs.Debug) > 0 {
		return c.Debug
	}
	var buf bytes.Buffer
	buf.WriteString("{")
	if c.ContextArgs.LocalID != nil {
		buf.WriteString(fmt.Sprintf(" LocalID:%q", c.LocalID))
	}
	if c.ContextArgs.RemoteID != nil {
		buf.WriteString(fmt.Sprintf(" RemoteID:%q", c.RemoteID))
	}
	if len(c.ContextArgs.Method) > 0 {
		buf.WriteString(fmt.Sprintf(" Method:%q", c.Method))
	}
	if len(c.ContextArgs.Name) > 0 {
		buf.WriteString(fmt.Sprintf(" Name:%q", c.Name))
	}
	if len(c.ContextArgs.Suffix) > 0 {
		buf.WriteString(fmt.Sprintf(" Suffix:%q", c.Suffix))
	}
	if c.ContextArgs.Label != 0 {
		buf.WriteString(fmt.Sprintf(" Label:%v", c.Label))
	}
	if len(c.ContextArgs.Discharges) > 0 {
		buf.WriteString(fmt.Sprintf(" #Discharges:%d", len(c.Discharges)))
	}
	buf.WriteString(" }")
	return buf.String()
}

func (c *context) Method() string                                { return c.ContextArgs.Method }
func (c *context) Name() string                                  { return c.ContextArgs.Name }
func (c *context) Suffix() string                                { return c.ContextArgs.Suffix }
func (c *context) Label() security.Label                         { return c.ContextArgs.Label }
func (c *context) CaveatDischarges() security.CaveatDischargeMap { return c.ContextArgs.Discharges }
func (c *context) LocalID() security.PublicID                    { return c.ContextArgs.LocalID }
func (c *context) RemoteID() security.PublicID                   { return c.ContextArgs.RemoteID }

// NewContext returns a new security.Context for the provided method, name,
// suffix, discharges, label and identities of the local and remote principals
// associated with an IPC invocation.
func NewContext(args ContextArgs) security.Context {
	return &context{ContextArgs: args}
}

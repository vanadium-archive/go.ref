package security

// This file provides an implementation of security.Authorizer.
//
// Definitions
//
// Trusted name: A trusted name is a name associated with a PublicID
// that does not begin with the wire.UntrustedIDProviderPrefix
// (see veyron/runtimes/google/security/wire.go).
//
// Self-RPC: An RPC request is said to be a "self-RPC" if the identities
// at the local and remote ends have a common trusted name.
// Ex: a client with name "veyron/alice" RPCing to a service with names
// "veyron/alice" and "google/alice".

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"veyron/runtimes/google/security/wire"

	"veyron2/security"
)

var (
	errACL          = errors.New("no matching ACL entry found")
	errIDAuthorizer = errors.New("no matching principal pattern found")
	errInvalidLabel = errors.New("label is invalid")
	errNilID        = errors.New("identity being matched is nil")
	errNilACL       = errors.New("ACL is nil")
)

// aclAuthorizer implements security.Authorizer.
type aclAuthorizer security.ACL

// Authorize verifies a request iff the identity at the remote end has a name authorized by the
// aclAuthorizer's ACL for the request's label, or the request corresponds to a self-RPC.
func (a aclAuthorizer) Authorize(ctx security.Context) error {
	if isSelfRPC(ctx) {
		return nil
	}
	return matchesACL(ctx.RemoteID(), ctx.Label(), security.ACL(a))
}

// NewACLAuthorizer creates an authorizer from the provided security.ACL. The
// authorizer authorizes a request iff the identity at the remote end has a name
// authorized by the provided ACL for the request's label, or the request
// corresponds to a self-RPC.
//
// Remark: During the life cycle of a request, an Authorizer is typically invoked
// right after validating all caveats on the remote identity and checking the
// trust-level of its identity provider. If the identity provider's trust-level is
// "Unknown" then the name of the identity is prepended with "untrusted/" (see the
// implementation of PublicID.Names). In light of this, it is recommended that the
// root names of all PrincipalPatterns, except "*", on the provided ACL are
// trusted for at least some public-key. This is because if this is not the case
// then the ACL would have superfluous patterns that cannot be matched by any
// identity.
func NewACLAuthorizer(acl security.ACL) security.Authorizer { return aclAuthorizer(acl) }

// fileACLAuthorizer implements security.Authorizer.
type fileACLAuthorizer string

// Authorize reads and decodes the fileACLAuthorizer's ACL file into a security.ACL
// and then verifies the request according to an aclAuthorizer based on the ACL. If
// reading or decoding the file fails then no requests are authorized.
func (a fileACLAuthorizer) Authorize(ctx security.Context) error {
	acl, err := loadACLFromFile(string(a))
	if err != nil {
		return err
	}
	return aclAuthorizer(acl).Authorize(ctx)
}

// NewFileACLAuthorizer creates an authorizer from the provided path to a file containing
// a JSON-encoded security.ACL. Each call to "Authorize" involves reading and decoding a
// security.ACL from the file and then authorizing the request according to the ACL. The
// authorizer monitors the file so out of band changes to the contents of the file are
// reflected in the ACL. If reading or decoding the file fails then no requests are authorized.
//
// The JSON-encoding of a security.ACL is essentially a JSON object describing a map from
// security.PrincipalPatterns to encoded security.LabelSets (see security.LabelSet.MarshalJSON).
// Examples:
// * `{"*" : "RW"}` encodes an ACL that allows all principals to access all methods with
//   security.ReadLabel or security.WriteLabel.
// * `{"veyron/alice": "RW", "veyron/bob/*": "R"} encodes an ACL that allows all principals
//   matching "veyron/alice" to access methods with security.ReadLabel or security.WriteLabel,
//   and all principals matching "veyron/bob/*" to access methods with security.ReadLabel.
//   (Also see security.PublicID.Match.)
//
// TODO(ataly, ashankar): Instead of reading the file on each call we should use the "inotify"
// mechanism to watch the file. Eventually we should also support ACLs stored in the Veyron store.
func NewFileACLAuthorizer(filePath string) security.Authorizer { return fileACLAuthorizer(filePath) }

// idAuthorizer implements security.Authorizer.
type idAuthorizer []security.PrincipalPattern

// Authorize verifies a request if the identity at the remote end matches one of the patterns
// specified within the idAuthorizer, or the request corresponds to a self-RPC.
func (a idAuthorizer) Authorize(ctx security.Context) error {
	if ctx.RemoteID() == nil {
		return errNilID
	}
	if isSelfRPC(ctx) {
		return nil
	}
	for _, p := range a {
		if ctx.RemoteID().Match(p) {
			return nil
		}
	}
	return errIDAuthorizer
}

// NewIDAuthorizer creates an authorizer from the provided security.PublicID. The resulting
// authorizer authorizes a request iff one of the following hold: (1) the identity at the
// remote end has a name matching one of the trusted names of either the provided identity
// or an identity blessed by the provided identity, OR (2) the request corresponds to a
// self-RPC.
func NewIDAuthorizer(id security.PublicID) security.Authorizer {
	if id == nil {
		return idAuthorizer(nil)
	}
	patterns := make([]security.PrincipalPattern, len(id.Names()))
	for i, n := range id.Names() {
		if !strings.HasPrefix(n, wire.UntrustedIDProviderPrefix) {
			patterns[i] = security.PrincipalPattern(n + "/*")
		}
	}
	return idAuthorizer(patterns)
}

func matchesACL(id security.PublicID, label security.Label, acl security.ACL) error {
	if id == nil {
		return errNilID
	}
	if acl == nil {
		return errNilACL
	}
	for key, labels := range acl {
		if labels.HasLabel(label) && id.Match(key) {
			return nil
		}
	}
	return errACL
}

// isSelfRPC returns true if the request described by the provided context corresponds
// to a self-RPC.
func isSelfRPC(ctx security.Context) bool {
	if ctx.RemoteID() == nil || ctx.LocalID() == nil {
		return false
	}
	remoteNames := map[string]bool{}
	for _, n := range ctx.RemoteID().Names() {
		if !strings.HasPrefix(n, wire.UntrustedIDProviderPrefix) {
			remoteNames[n] = true
		}
	}
	for _, n := range ctx.LocalID().Names() {
		if remoteNames[n] {
			return true
		}
	}
	return false
}

func loadACLFromFile(filePath string) (security.ACL, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return loadACL(f)
}

func loadACL(r io.Reader) (security.ACL, error) {
	var acl security.ACL
	if err := json.NewDecoder(r).Decode(&acl); err != nil {
		return nil, err
	}
	return acl, nil
}

func saveACL(w io.Writer, acl security.ACL) error {
	return json.NewEncoder(w).Encode(acl)
}

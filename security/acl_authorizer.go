package security

// This file provides an implementation of security.Authorizer.
//
// Definitions
// * Self-RPC: An RPC request is said to be a "self-RPC" if the identities
// at the local and remote ends are identical.

import (
	"errors"
	"os"
	"reflect"

	"veyron2/security"
)

var (
	errACL          = errors.New("no matching ACL entry found")
	errInvalidLabel = errors.New("label is invalid")
	errNilID        = errors.New("identity being matched is nil")
	nullACL         security.ACL
)

// aclAuthorizer implements Authorizer.
type aclAuthorizer security.ACL

// Authorize verifies a request iff the identity at the remote end has a name authorized by
// the aclAuthorizer's ACL for the request's label, or the request corresponds to a self-RPC.
func (a aclAuthorizer) Authorize(ctx security.Context) error {
	// Test if the request corresponds to a self-RPC.
	if ctx.LocalID() != nil && ctx.RemoteID() != nil && reflect.DeepEqual(ctx.LocalID(), ctx.RemoteID()) {
		return nil
	}
	// Match the aclAuthorizer's ACL.
	return matchesACL(ctx.RemoteID(), ctx.Label(), security.ACL(a))
}

// NewACLAuthorizer creates an authorizer from the provided ACL. The
// authorizer authorizes a request iff the identity at the remote end has a name
// authorized by the provided ACL for the request's label, or the request
// corresponds to a self-RPC.
func NewACLAuthorizer(acl security.ACL) security.Authorizer { return aclAuthorizer(acl) }

// fileACLAuthorizer implements Authorizer.
type fileACLAuthorizer string

// Authorize reads and decodes the fileACLAuthorizer's ACL file into a ACL and
// then verifies the request according to an aclAuthorizer based on the ACL. If
// reading or decoding the file fails then no requests are authorized.
func (a fileACLAuthorizer) Authorize(ctx security.Context) error {
	acl, err := loadACLFromFile(string(a))
	if err != nil {
		return err
	}
	return aclAuthorizer(acl).Authorize(ctx)
}

// NewFileACLAuthorizer creates an authorizer from the provided path to a file
// containing a JSON-encoded ACL. Each call to "Authorize" involves reading and
// decoding a ACL from the file and then authorizing the request according to the
// ACL. The authorizer monitors the file so out of band changes to the contents of
// the file are reflected in the ACL. If reading or decoding the file fails then
// no requests are authorized.
//
// The JSON-encoding of a ACL is essentially a JSON object describing a map from
// PrincipalPatterns to encoded LabelSets (see LabelSet.MarshalJSON).
// Examples:
// * `{"*" : "RW"}` encodes an ACL that allows all principals to access all methods with
//   ReadLabel or WriteLabel.
// * `{"veyron/alice": "RW", "veyron/bob/*": "R"} encodes an ACL that allows all principals
//   matching "veyron/alice" to access methods with ReadLabel or WriteLabel,
//   and all principals matching "veyron/bob/*" to access methods with ReadLabel.
//   (Also see PublicID.Match.)
//
// TODO(ataly, ashankar): Instead of reading the file on each call we should use the "inotify"
// mechanism to watch the file. Eventually we should also support ACLs stored in the Veyron
// store.
func NewFileACLAuthorizer(filePath string) security.Authorizer { return fileACLAuthorizer(filePath) }

func matchesACL(id security.PublicID, label security.Label, acl security.ACL) error {
	if id == nil {
		return errNilID
	}
	if acl.Matches(id, label) {
		return nil
	}
	return errACL
}

func loadACLFromFile(filePath string) (security.ACL, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nullACL, err
	}
	defer f.Close()
	return security.LoadACL(f)
}

// Package acls provides a library to assist servers implementing
// GetACL/SetACL functions.
package acls

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"

	"v.io/v23/services/security/access"
)

// ComputeEtag produces the tag value returned by access.GetACL() (per
// veyron2/services/security/access/service.vdl) that GetACL()/SetACL()
// use to determine if the ACLs have been asynchronously modified.
func ComputeEtag(acl access.TaggedACLMap) (string, error) {
	b := new(bytes.Buffer)
	if err := acl.WriteTo(b); err != nil {
		return "", err
	}

	md5hash := md5.Sum(b.Bytes())
	etag := hex.EncodeToString(md5hash[:])
	return etag, nil
}

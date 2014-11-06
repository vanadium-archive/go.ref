package acl

import (
	"encoding/json"
	"io"

	"veyron.io/veyron/veyron2/security"
)

// Includes returns true iff the ACL grants access to a principal
// that presents blessings.
func (acl ACL) Includes(blessings ...string) bool {
	blessings = pruneBlacklisted(acl.NotIn, blessings)
	for _, pattern := range acl.In {
		if pattern.MatchedBy(blessings...) {
			return true
		}
	}
	return false
}

// WriteTo writes the JSON-encoded representation of a TaggedACLMap to w.
func (m TaggedACLMap) WriteTo(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

// ReadTaggedACLMap reads the JSON-encoded representation of a TaggedACLMap from r.
func ReadTaggedACLMap(r io.Reader) (m TaggedACLMap, err error) {
	err = json.NewDecoder(r).Decode(&m)
	return
}

func pruneBlacklisted(blacklist, blessings []string) []string {
	if len(blacklist) == 0 {
		return blessings
	}
	var filtered []string
	for _, b := range blessings {
		if !security.BlessingPattern(b).MatchedBy(blacklist...) {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

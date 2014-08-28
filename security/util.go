package security

import (
	"encoding/base64"
	"encoding/json"
	"io"

	"veyron2/security"
	"veyron2/vom"
)

var nullACL security.ACL

// OpenACL creates an ACL that grants access to all principals.
func OpenACL() security.ACL {
	acl := security.ACL{}
	acl.In = map[security.BlessingPattern]security.LabelSet{security.AllPrincipals: security.AllLabels}
	return acl
}

// LoadIdentity reads a PrivateID from r, assuming that it was written using
// SaveIdentity.
func LoadIdentity(r io.Reader) (security.PrivateID, error) {
	var id security.PrivateID
	if err := vom.NewDecoder(base64.NewDecoder(base64.URLEncoding, r)).Decode(&id); err != nil {
		return nil, err
	}
	return id, nil
}

// SaveIdentity writes a serialized form of a PrivateID to w, which can be
// recovered using LoadIdentity.
func SaveIdentity(w io.Writer, id security.PrivateID) error {
	closer := base64.NewEncoder(base64.URLEncoding, w)
	if err := vom.NewEncoder(closer).Encode(id); err != nil {
		return err
	}
	// Must close the base64 encoder to flush out any partially written blocks.
	if err := closer.Close(); err != nil {
		return err
	}
	return nil
}

// LoadACL reads an ACL from the provided Reader containing a JSON encoded ACL.
func LoadACL(r io.Reader) (security.ACL, error) {
	var acl security.ACL
	if err := json.NewDecoder(r).Decode(&acl); err != nil {
		return nullACL, err
	}
	return acl, nil
}

// SaveACL encodes an ACL in JSON format and writes it to the provided Writer.
func SaveACL(w io.Writer, acl security.ACL) error {
	return json.NewEncoder(w).Encode(acl)
}

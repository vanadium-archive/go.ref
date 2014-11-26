// Package flag defines a method for parsing ACL flags and constructing
// a security.Authorizer based on them.
package flag

import (
	"bytes"
	"errors"
	"flag"

	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/security/access"
)

var (
	acl     = flag.String("acl", "", `acl is an optional JSON-encoded access.TaggedACLMap that is used to construct a security.Authorizer using the tags defined in the access package. Example: {"Read": {"In": ["veyron/alice/..."]}} allows all delegates of "veyron/alice" to access all methods with the "Read" access tag on them. If this flag is set then the --acl_file flag must not be set.`)
	aclFile = flag.String("acl_file", "", "acl_file is an optional path to a file containing a JSON-encoded access.TaggedACLMap that is used to construct a security.Authorizer (with the set of tags defined in the access package). If this flag is set then --acl must not be set")
)

// NewAuthorizerOrDie constructs an Authorizer based on the provided "--acl" or
// "--acl_file" flags. If both flags are provided the function panics, and if
// neither flag is provided a nil Authorizer is returned (Note that services with
// nil Authorizers are provided with default authorization by the framework.)
func NewAuthorizerOrDie() security.Authorizer {
	if len(*acl) == 0 && len(*aclFile) == 0 {
		return nil
	}
	if len(*acl) != 0 && len(*aclFile) != 0 {
		panic(errors.New("only one of the flags \"--acl\" or \"--acl_file\" must be provided"))
	}
	var a security.Authorizer
	var err error
	if len(*aclFile) != 0 {
		a, err = access.TaggedACLAuthorizerFromFile(*aclFile, access.TypicalTagType())
	} else {
		var tam access.TaggedACLMap
		if tam, err = access.ReadTaggedACLMap(bytes.NewBufferString(*acl)); err == nil {
			a, err = access.TaggedACLAuthorizer(tam, access.TypicalTagType())
		}
	}
	if err != nil {
		panic(err)
	}
	return a
}

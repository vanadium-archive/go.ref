// Package flag defines a method for parsing ACL flags and constructing
// a security.Authorizer based on them.
package flag

import (
	"bytes"
	"errors"
	"flag"

	vsecurity "veyron.io/veyron/veyron/security"

	"veyron.io/veyron/veyron2/security"
)

var (
	acl     = flag.String("acl", "", "acl is an optional JSON-encoded security.ACL that is used to construct a security.Authorizer. Example: \"{\"veyron.io/veyron/veyron/alice\":\"RW\"}\" is a JSON-encoded ACL that allows all principals matching \"veyron.io/veyron/veyron/alice\" to access all methods with ReadLabel or WriteLabel. If this flag is provided then the \"--acl_file\" must be absent.")
	aclFile = flag.String("acl_file", "", "acl_file is an optional path to a file containing a JSON-encoded security.ACL that is used to construct a security.Authorizer. If this flag is provided then the \"--acl_file\" flag must be absent.")
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
	if len(*aclFile) != 0 {
		return vsecurity.NewFileACLAuthorizer(*aclFile)
	}
	a, err := vsecurity.LoadACL(bytes.NewBufferString(*acl))
	if err != nil {
		return nil
	}
	return vsecurity.NewACLAuthorizer(a)
}

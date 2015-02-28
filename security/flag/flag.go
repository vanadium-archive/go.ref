// Package flag defines a method for parsing ACL flags and constructing
// a security.Authorizer based on them.
package flag

import (
	"bytes"
	"errors"
	"flag"
	"os"

	"v.io/v23/security"
	"v.io/v23/services/security/access"

	"v.io/core/veyron/lib/flags"
)

var authFlags *flags.Flags

func init() {
	authFlags = flags.CreateAndRegister(flag.CommandLine, flags.ACL)
}

// NewAuthorizerOrDie constructs an Authorizer based on the provided "--veyron.acl.literal" or
// "--veyron.acl.file" flags. Otherwise it creates a default Authorizer.
func NewAuthorizerOrDie() security.Authorizer {
	flags := authFlags.ACLFlags()
	fname := flags.ACLFile("runtime")
	literal := flags.ACLLiteral()

	if fname == "" && literal == "" {
		return nil
	}
	var a security.Authorizer
	var err error
	if literal == "" {
		a, err = access.TaggedACLAuthorizerFromFile(fname, access.TypicalTagType())
	} else {
		var tam access.TaggedACLMap
		if tam, err = access.ReadTaggedACLMap(bytes.NewBufferString(literal)); err == nil {
			a, err = access.TaggedACLAuthorizer(tam, access.TypicalTagType())
		}
	}
	if err != nil {
		panic(err)
	}
	return a
}

// TODO(rjkroege): Refactor these two functions into one by making an Authorizer
// use a TaggedACLMap accessor interface.
// TaggedACLMapFromFlag reads the same flags as NewAuthorizerOrDie but
// produces a TaggedACLMap for callers that need more control of how ACLs
// are managed.
func TaggedACLMapFromFlag() (access.TaggedACLMap, error) {
	flags := authFlags.ACLFlags()
	fname := flags.ACLFile("runtime")
	literal := flags.ACLLiteral()

	if fname == "" && literal == "" {
		return nil, nil
	}

	if literal == "" {
		file, err := os.Open(fname)
		if err != nil {
			return nil, errors.New("cannot open argument to --veyron.acl.file " + fname)
		}
		defer file.Close()
		return access.ReadTaggedACLMap(file)
	} else {
		return access.ReadTaggedACLMap(bytes.NewBufferString(literal))
	}
}

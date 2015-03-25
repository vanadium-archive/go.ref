// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package flag defines a method for parsing AccessList flags and constructing
// a security.Authorizer based on them.
package flag

import (
	"bytes"
	"errors"
	"flag"
	"os"

	"v.io/v23/security"
	"v.io/v23/services/security/access"

	"v.io/x/ref/lib/flags"
)

var authFlags *flags.Flags

func init() {
	authFlags = flags.CreateAndRegister(flag.CommandLine, flags.AccessList)
}

// NewAuthorizerOrDie constructs an Authorizer based on the provided "--veyron.acl.literal" or
// "--veyron.acl.file" flags. Otherwise it creates a default Authorizer.
func NewAuthorizerOrDie() security.Authorizer {
	flags := authFlags.AccessListFlags()
	fname := flags.AccessListFile("runtime")
	literal := flags.AccessListLiteral()

	if fname == "" && literal == "" {
		return nil
	}
	var a security.Authorizer
	var err error
	if literal == "" {
		a, err = access.PermissionsAuthorizerFromFile(fname, access.TypicalTagType())
	} else {
		var tam access.Permissions
		if tam, err = access.ReadPermissions(bytes.NewBufferString(literal)); err == nil {
			a, err = access.PermissionsAuthorizer(tam, access.TypicalTagType())
		}
	}
	if err != nil {
		panic(err)
	}
	return a
}

// TODO(rjkroege): Refactor these two functions into one by making an Authorizer
// use a Permissions accessor interface.
// PermissionsFromFlag reads the same flags as NewAuthorizerOrDie but
// produces a Permissions for callers that need more control of how AccessLists
// are managed.
func PermissionsFromFlag() (access.Permissions, error) {
	flags := authFlags.AccessListFlags()
	fname := flags.AccessListFile("runtime")
	literal := flags.AccessListLiteral()

	if fname == "" && literal == "" {
		return nil, nil
	}

	if literal == "" {
		file, err := os.Open(fname)
		if err != nil {
			return nil, errors.New("cannot open argument to --veyron.acl.file " + fname)
		}
		defer file.Close()
		return access.ReadPermissions(file)
	} else {
		return access.ReadPermissions(bytes.NewBufferString(literal))
	}
}

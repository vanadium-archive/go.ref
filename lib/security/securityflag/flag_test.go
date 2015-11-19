// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package securityflag

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/ref/test/modules"
)

//go:generate jiri test generate

var (
	perms1 = access.Permissions{}
	perms2 = access.Permissions{
		string(access.Read): access.AccessList{
			In: []security.BlessingPattern{"v23:alice:$", "v23:bob"},
		},
		string(access.Write): access.AccessList{
			In: []security.BlessingPattern{"v23:alice:$"},
		},
	}

	expectedAuthorizer = map[string]security.Authorizer{
		"empty":  access.TypicalTagTypePermissionsAuthorizer(perms1),
		"perms2": access.TypicalTagTypePermissionsAuthorizer(perms2),
	}
)

var permFromFlag = modules.Register(func(env *modules.Env, args ...string) error {
	nfargs := flag.CommandLine.Args()
	perms, err := PermissionsFromFlag()
	if err != nil {
		fmt.Fprintf(env.Stdout, "PermissionsFromFlag() failed: %v", err)
		return nil
	}
	got := access.TypicalTagTypePermissionsAuthorizer(perms)
	want := expectedAuthorizer[nfargs[0]]
	if !reflect.DeepEqual(got, want) {
		fmt.Fprintf(env.Stdout, "args %#v\n", args)
		fmt.Fprintf(env.Stdout, "AuthorizerFromFlags() got Authorizer: %v, want: %v", got, want)
	}
	return nil
}, "permFromFlag")

func writePermissionsToFile(perms access.Permissions) (string, error) {
	f, err := ioutil.TempFile("", "permissions")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := access.WritePermissions(f, perms); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func TestNewAuthorizerOrDie(t *testing.T) {
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	// Create a file.
	filename, err := writePermissionsToFile(perms2)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(filename)

	testdata := []struct {
		prog  modules.Program
		flags []string
		auth  string
	}{
		{
			prog:  permFromFlag,
			flags: []string{"--v23.permissions.file", "runtime:" + filename},
			auth:  "perms2",
		},
		{
			prog:  permFromFlag,
			flags: []string{"--v23.permissions.literal", "{}"},
			auth:  "empty",
		},
		{
			prog:  permFromFlag,
			flags: []string{"--v23.permissions.literal", `{"Read": {"In":["v23:alice:$", "v23:bob"]}, "Write": {"In":["v23:alice:$"]}}`},
			auth:  "perms2",
		},
	}
	for _, td := range testdata {
		fp := append(td.flags, td.auth)
		h, err := sh.Start(nil, td.prog, fp...)
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		b := new(bytes.Buffer)
		h.Shutdown(b, os.Stderr)
		if got := b.String(); got != "" {
			t.Errorf(got)
		}
	}
}

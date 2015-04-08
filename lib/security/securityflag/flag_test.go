// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package securityflag

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/x/ref/test/modules"
)

//go:generate v23 test generate

var (
	perms1 = access.Permissions{}
	perms2 = access.Permissions{
		string(access.Read): access.AccessList{
			In: []security.BlessingPattern{"v23/alice/$", "v23/bob"},
		},
		string(access.Write): access.AccessList{
			In: []security.BlessingPattern{"v23/alice/$"},
		},
	}

	expectedAuthorizer = map[string]security.Authorizer{
		"empty":  auth(access.PermissionsAuthorizer(perms1, access.TypicalTagType())),
		"perms2": auth(access.PermissionsAuthorizer(perms2, access.TypicalTagType())),
	}
)

func auth(a security.Authorizer, err error) security.Authorizer {
	if err != nil {
		panic(err)
	}
	return a
}

func tamFromFlag(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	nfargs := flag.CommandLine.Args()
	tam, err := PermissionsFromFlag()
	if err != nil {
		fmt.Fprintf(stdout, "PermissionsFromFlag() failed: %v", err)
		return nil
	}
	got := auth(access.PermissionsAuthorizer(tam, access.TypicalTagType()))
	want := expectedAuthorizer[nfargs[0]]
	if !reflect.DeepEqual(got, want) {
		fmt.Fprintf(stdout, "args %#v\n", args)
		fmt.Fprintf(stdout, "AuthorizerFromFlags() got Authorizer: %v, want: %v", got, want)
	}
	return nil
}

func writePermissionsToFile(perms access.Permissions) (string, error) {
	f, err := ioutil.TempFile("", "permissions")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := perms.WriteTo(f); err != nil {
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
		cmd   string
		flags []string
		auth  string
	}{
		{
			cmd:   "tamFromFlag",
			flags: []string{"--v23.permissions.file", "runtime:" + filename},
			auth:  "perms2",
		},
		{
			cmd:   "tamFromFlag",
			flags: []string{"--v23.permissions.literal", "{}"},
			auth:  "empty",
		},
		{
			cmd:   "tamFromFlag",
			flags: []string{"--v23.permissions.literal", `{"Read": {"In":["v23/alice/$", "v23/bob"]}, "Write": {"In":["v23/alice/$"]}}`},
			auth:  "perms2",
		},
	}

	for _, td := range testdata {
		fp := append(td.flags, td.auth)
		h, err := sh.Start(td.cmd, nil, fp...)
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

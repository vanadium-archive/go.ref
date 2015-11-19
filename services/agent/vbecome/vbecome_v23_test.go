// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/v23tests"
)

//go:generate jiri test generate .

func writeRoledConfig() (path string, shutdown func(), err error) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		return dir, nil, err
	}
	err = ioutil.WriteFile(filepath.Join(dir, "therole.conf"), []byte(`
{
  "Members": ["root:child"],
  "Extend": true
}
`), 0644)
	return dir, func() { os.RemoveAll(dir) }, err
}

func V23TestBecomeRole(t *v23tests.T) {
	vbecome := t.BuildV23Pkg("v.io/x/ref/services/agent/vbecome")
	principal := t.BuildV23Pkg("v.io/x/ref/cmd/principal")

	roled := t.BuildV23Pkg("v.io/x/ref/services/role/roled")
	roledCreds, _ := t.Shell().NewChildCredentials("master")
	roled = roled.WithStartOpts(roled.StartOpts().WithCustomCredentials(roledCreds))

	v23tests.RunRootMT(t, "--v23.tcp.address=127.0.0.1:0")

	dir, shutdown, err := writeRoledConfig()
	if err != nil {
		t.Fatalf("Couldn't write roled config: %v", err)
	}
	defer shutdown()
	roled.Start("--v23.tcp.address=127.0.0.1:0", "--config-dir", dir, "--name", "roled")

	output := vbecome.Run("--role=roled/therole", principal.Path(), "dump")
	want := regexp.MustCompile(`Default Blessings\s+root:master:therole:root:child`)
	if !want.MatchString(output) {
		t.Errorf("Principal didn't have the role blessing:\n %s", output)
	}
}

func V23TestBecomeName(t *v23tests.T) {
	vbecome := t.BuildV23Pkg("v.io/x/ref/services/agent/vbecome")
	principal := t.BuildV23Pkg("v.io/x/ref/cmd/principal")

	v23tests.RunRootMT(t, "--v23.tcp.address=127.0.0.1:0")
	output := vbecome.Run("--name=bob", principal.Path(), "dump")
	want := regexp.MustCompile(`Default Blessings\s+root:child:bob`)
	if !want.MatchString(output) {
		t.Errorf("Principal didn't have the expected blessing:\n %s", output)
	}
}

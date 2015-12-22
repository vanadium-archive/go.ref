// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build wspr
//
// We restrict to a special build-tag since it's required by wsprlib.

// Runs the servicerunner binary and checks that it outputs a JSON line to
// stdout with the expected variables.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"testing"

	"v.io/x/ref"
)

func TestServiceRunner(t *testing.T) {
	ref.EnvClearCredentials()
	tmpdir, err := ioutil.TempDir("", "servicerunner_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)
	os.Setenv("TMPDIR", tmpdir)

	bin := path.Join(tmpdir, "servicerunner")
	fmt.Println("Building", bin)
	err = exec.Command("jiri", "go", "build", "-o", bin, "-a", "-tags", "wspr", "v.io/x/ref/cmd/servicerunner").Run()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}

	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	if err = json.Unmarshal(line, &vars); err != nil {
		t.Fatal(err)
	}
	fmt.Println(vars)
	expectedVars := []string{
		"MT_NAME",
		"PROXY_NAME",
		"WSPR_ADDR",
		"TEST_IDENTITYD_NAME",
		"TEST_IDENTITYD_HTTP_ADDR",
	}
	for _, name := range expectedVars {
		if _, ok := vars[name]; !ok {
			t.Error("Missing", name)
		}
	}

	if err != cmd.Process.Kill() {
		t.Fatal(err)
	}
}

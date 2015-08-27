// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lockfile

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"v.io/x/ref/test/modules"

	_ "v.io/x/ref/runtime/factories/generic"
)

//go:generate v23 test generate

var createLockfile = modules.Register(func(env *modules.Env, args ...string) error {
	dir := args[0]
	err := CreateLockfile(dir)
	if err == nil {
		fmt.Println("Grabbed lock")
	} else {
		fmt.Println("Lock failed")
	}
	return err
}, "createLockfile")

func TestLockFile(t *testing.T) {
	dir, err := ioutil.TempDir("", "lf")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err = CreateLockfile(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfileName)
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err, running := StillRunning(bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !running {
		t.Fatal("expected StillRunning() = true")
	}

	if err = CreateLockfile(dir); err == nil {
		t.Fatal("Creating 2nd lockfile should fail")
	}

	RemoveLockfile(dir)
	_, err = os.Lstat(path)
	if !os.IsNotExist(err) {
		t.Fatalf("%s: expected NotExist, got %v", path, err)
	}
}

func TestOtherProcess(t *testing.T) {
	dir, err := ioutil.TempDir("", "lf")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatal(err)
	}

	// Start a new child which creates a lockfile and exits.
	h, err := sh.Start(nil, createLockfile, dir)
	if err != nil {
		t.Fatal(err)
	}
	h.Expect("Grabbed lock")
	h.Shutdown(os.Stdout, os.Stderr)
	if h.Failed() {
		t.Fatal(h.Error())
	}

	// Verify it created a lockfile.
	path := filepath.Join(dir, lockfileName)
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// And that we know the lockfile is invalid.
	err, running := StillRunning(bytes)
	if err != nil {
		t.Fatal(err)
	}
	if running {
		t.Fatal("child process is dead")
	}

	// Now create a lockfile for the process.
	if err = CreateLockfile(dir); err != nil {
		t.Fatal(err)
	}

	// Now the child should fail to create one.
	h, err = sh.Start(nil, createLockfile, dir)
	if err != nil {
		t.Fatal(err)
	}
	h.Expect("Lock failed")
	h.Shutdown(os.Stderr, os.Stderr)
	if h.Failed() {
		t.Fatal(h.Error())
	}
}

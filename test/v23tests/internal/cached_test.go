// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/test/v23tests"
)

//go:generate jiri test generate

func init() {
	dir, err := ioutil.TempDir("./", "v23test-internal")
	if err != nil {
		panic(err.Error())
	}
	os.Setenv("V23_BIN_DIR", dir)
	tmpDir = dir
}

var tmpDir string
var modTimes []time.Time

// build build's a binary and appends it's modtime to the
// global slice modTimes
func build(i *v23tests.T) {
	nsBin := i.BuildGoPkg("v.io/x/ref/cmd/namespace")
	fi, err := os.Stat(nsBin.Path())
	if err != nil {
		i.Fatal(err)
	}
	modTimes = append(modTimes, fi.ModTime())
}

func fmtTimes() string {
	r := ""
	for _, t := range modTimes {
		r += t.String() + "\n"
	}
	return r
}

// checkTimes returns true if modTimes has at least one value
// and the values are all the same.
func checkTimes(i *v23tests.T) bool {
	_, file, line, _ := runtime.Caller(1)
	loc := fmt.Sprintf("%s:%d", filepath.Dir(file), line)
	if len(modTimes) == 0 {
		i.Fatalf("%s: nothing has been built", loc)
	}
	first := modTimes[0]
	for _, t := range modTimes[1:] {
		if t != first {
			i.Fatalf("%s: binary not cached: build times: %s", loc, fmtTimes())
		}
	}
	i.Logf("%d entries", len(modTimes))
	return true
}

func V23TestOne(i *v23tests.T) {
	build(i)
	checkTimes(i)
}

func V23TestTwo(i *v23tests.T) {
	build(i)
	checkTimes(i)
}

func V23TestThree(i *v23tests.T) {
	build(i)
	checkTimes(i)
}

func V23TestFour(i *v23tests.T) {
	build(i)
	checkTimes(i)
}

func TestMain(m *testing.M) {
	r := m.Run()
	if len(tmpDir) > 0 {
		os.RemoveAll(tmpDir)
	}
	os.Exit(r)
}

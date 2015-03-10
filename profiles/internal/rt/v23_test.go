// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY
package rt_test

import "fmt"
import "testing"
import "os"

import "v.io/x/ref/lib/modules"
import "v.io/x/ref/lib/testutil"

func init() {
	modules.RegisterChild("noWaiters", ``, noWaiters)
	modules.RegisterChild("forceStop", ``, forceStop)
	modules.RegisterChild("app", ``, app)
	modules.RegisterChild("child", ``, child)
	modules.RegisterChild("principal", ``, principal)
	modules.RegisterChild("runner", `Runner runs a principal as a subprocess and reports back with its
own security info and it's childs.`, runner)
	modules.RegisterChild("complexServerProgram", `complexServerProgram demonstrates the recommended way to write a more
complex server application (with several servers, a mix of interruptible
and blocking cleanup, and parallel and sequential cleanup execution).
For a more typical server, see simpleServerProgram.`, complexServerProgram)
	modules.RegisterChild("simpleServerProgram", `simpleServerProgram demonstrates the recommended way to write a typical
simple server application (with one server and a clean shutdown triggered by
a signal or a stop command).  For an example of something more involved, see
complexServerProgram.`, simpleServerProgram)
	modules.RegisterChild("withRuntime", ``, withRuntime)
	modules.RegisterChild("withoutRuntime", ``, withoutRuntime)
}

func TestMain(m *testing.M) {
	testutil.Init()
	if modules.IsModulesChildProcess() {
		if err := modules.Dispatch(); err != nil {
			fmt.Fprintf(os.Stderr, "modules.Dispatch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

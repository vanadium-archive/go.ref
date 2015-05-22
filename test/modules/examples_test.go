// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules_test

import (
	"fmt"
	"os"

	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
)

var Echo = modules.Register(func(env *modules.Env, args ...string) error {
	for i, a := range args {
		fmt.Fprintf(env.Stdout, "%d: %s\n", i, a)
	}
	return nil
}, "echo")

func ExampleDispatch() {
	if modules.IsChildProcess() {
		// Child process dispatches to the echo program.
		if err := modules.Dispatch(); err != nil {
			panic(err)
		}
		return
	}
	// Parent process spawns the echo program.
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	sh, _ := modules.NewShell(ctx, nil, false, nil)
	defer sh.Cleanup(nil, nil)
	h, _ := sh.Start(nil, Echo, "a", "b")
	h.Shutdown(os.Stdout, os.Stderr)
	// Output:
	// 0: a
	// 1: b
}

func ExampleDispatchAndExitIfChild() {
	// Child process dispatches to the echo program.
	modules.DispatchAndExitIfChild()
	// Parent process spawns the echo program.
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	sh, _ := modules.NewShell(ctx, nil, false, nil)
	defer sh.Cleanup(nil, nil)
	h, _ := sh.Start(nil, Echo, "c", "d")
	h.Shutdown(os.Stdout, os.Stderr)
	// Output:
	// 0: c
	// 1: d
}

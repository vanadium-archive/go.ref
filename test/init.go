// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"flag"
	"os"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/x/ref/internal/logger"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/test/testutil"
)

const (
	TestBlessing = "test-blessing"
)

var once sync.Once
var IntegrationTestsEnabled bool
var IntegrationTestsDebugShellOnError bool

const IntegrationTestsFlag = "v23.tests"
const IntegrationTestsDebugShellOnErrorFlag = "v23.tests.shell-on-fail"

func init() {
	flag.BoolVar(&IntegrationTestsEnabled, IntegrationTestsFlag, false, "Run integration tests.")
	flag.BoolVar(&IntegrationTestsDebugShellOnError, IntegrationTestsDebugShellOnErrorFlag, false, "Drop into a debug shell if an integration test fails.")
}

// V23Init initializes the runtime and sets up the principal with the
// self-signed TestBlessing. The blessing setup step is skipped if this function
// is invoked from a subprocess run using the modules package; in that case, the
// blessings of the principal are created by the parent process.
// NOTE: For tests involving Vanadium RPCs, developers are encouraged to use
// V23InitWithMounttable, and have their services access each other via the
// mount table (rather than using endpoint strings).
func V23Init() (*context.T, v23.Shutdown) {
	return initImpl(false)
}

// V23InitWithMounttable initializes the runtime and:
// - Sets up the principal with the self-signed TestBlessing
// - Starts a mounttable and sets the namespace roots appropriately
// Both these steps are skipped if this function is invoked from a subprocess
// run using the modules package; in that case, the mounttable to use and the
// blessings of the principal are created by the parent process.
func V23InitWithMounttable() (*context.T, v23.Shutdown) {
	return initImpl(true)
}

// initImpl initializes the runtime and returns a new context and shutdown
// function.
func initImpl(createMounttable bool) (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init()
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{Protocol: "tcp", Address: "127.0.0.1:0"}}})

	modulesProcess := os.Getenv("V23_SHELL_HELPER_PROCESS_ENTRY_POINT") != ""

	if !modulesProcess {
		var err error
		if ctx, err = v23.WithPrincipal(ctx, testutil.NewPrincipal(TestBlessing)); err != nil {
			panic(err)
		}
	}

	ns := v23.GetNamespace(ctx)
	ns.CacheCtl(naming.DisableCache(true))

	if !modulesProcess && createMounttable {
		disp, err := mounttablelib.NewMountTableDispatcher(ctx, "", "", "mounttable")
		if err != nil {
			panic(err)
		}
		_, s, err := v23.WithNewDispatchingServer(ctx, "", disp, options.ServesMountTable(true))
		if err != nil {
			panic(err)
		}
		ns.SetRoots(s.Status().Endpoints[0].Name())
	}

	return ctx, shutdown
}

// TestContext returns a *context.T suitable for use in tests. It sets the
// context's logger to logger.Global(), and that's it. In particular, it does
// not call v23.Init().
func TestContext() (*context.T, context.CancelFunc) {
	ctx, cancel := context.RootContext()
	return context.WithLogger(ctx, logger.Global()), cancel
}

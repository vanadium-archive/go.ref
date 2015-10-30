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

// V23Init initializes the runtime and sets up some convenient infrastructure for tests:
// - Sets a freshly created principal (with a single self-signed blessing) on it.
// - Creates a mounttable and sets the namespace roots appropriately
// Both steps are skipped if this function is invoked from a process run
// using the modules package.
func V23Init() (*context.T, v23.Shutdown) {
	moduleProcess := os.Getenv("V23_SHELL_HELPER_PROCESS_ENTRY_POINT") != ""
	return initWithParams(initParams{
		CreatePrincipal:  !moduleProcess,
		CreateMounttable: !moduleProcess,
	})
}

// initParams contains parameters for tests that need to control what happens during
// init carefully.
type initParams struct {
	CreateMounttable bool // CreateMounttable creates a new mounttable.
	CreatePrincipal  bool // CreatePrincipal creates a new principal with self-signed blessing.
}

// initWithParams initializes the runtime and returns a new context and shutdown function.
// Specific aspects of initialization can be controlled via the params struct.
func initWithParams(params initParams) (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init()
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{Protocol: "tcp", Address: "127.0.0.1:0"}}})
	if params.CreatePrincipal {
		var err error
		if ctx, err = v23.WithPrincipal(ctx, testutil.NewPrincipal(TestBlessing)); err != nil {
			panic(err)
		}
	}
	ns := v23.GetNamespace(ctx)
	ns.CacheCtl(naming.DisableCache(true))
	if params.CreateMounttable {
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

// TestContext returns a *contect.T suitable for use in tests with logging
// configured to use loggler.Global(), but nothing else. In particular it does
// not call v23.Init and hence any of the v23 functions that
func TestContext() (*context.T, context.CancelFunc) {
	ctx, cancel := context.RootContext()
	return context.WithLogger(ctx, logger.Global()), cancel
}

// V23InitSimple is like V23Init, except that it does not setup a
// mounttable.
func V23InitSimple() (*context.T, v23.Shutdown) {
	return initWithParams(initParams{
		CreatePrincipal:  true,
		CreateMounttable: false,
	})
}

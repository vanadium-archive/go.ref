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

// internalInit initializes the runtime and returns a new context.
func internalInit(ctx *context.T, createMounttable bool) *context.T {
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{Protocol: "tcp", Address: "127.0.0.1:0"}}})

	v23testProcess := os.Getenv("V23_SHELL_TEST_PROCESS") != ""

	if !v23testProcess {
		var err error
		if ctx, err = v23.WithPrincipal(ctx, testutil.NewPrincipal(TestBlessing)); err != nil {
			panic(err)
		}
	}

	ns := v23.GetNamespace(ctx)
	ns.CacheCtl(naming.DisableCache(true))

	if !v23testProcess && createMounttable {
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

	return ctx
}

// TestContext returns a *context.T suitable for use in tests. It sets the
// context's logger to logger.Global(), and that's it. In particular, it does
// not call v23.Init().
func TestContext() (*context.T, context.CancelFunc) {
	ctx, cancel := context.RootContext()
	return context.WithLogger(ctx, logger.Global()), cancel
}

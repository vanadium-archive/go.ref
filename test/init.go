// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"flag"
	"os"
	"runtime"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"

	"v.io/x/ref/internal/logger"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/xrpc"
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

// Init sets up state for running tests: Adjusting GOMAXPROCS,
// configuring the vlog logging library, setting up the random number generator
// etc.
//
// Doing so requires flags to be parse, so this function explicitly parses
// flags. Thus, it is NOT a good idea to call this from the init() function
// of any module except "main" or _test.go files.
func Init() {
	init := func() {
		if os.Getenv("GOMAXPROCS") == "" {
			// Set the number of logical processors to the number of CPUs,
			// if GOMAXPROCS is not set in the environment.
			runtime.GOMAXPROCS(runtime.NumCPU())
		}
		flags.SetDefaultProtocol("tcp")
		flags.SetDefaultHostPort("127.0.0.1:0")
		flags.SetDefaultNamespaceRoot("/127.0.0.1:8101")
		// At this point all of the flags that we're going to use for
		// tests must be defined.
		// This will be the case if this is called from the init()
		// function of a _test.go file.
		flag.Parse()
		logger.Manager(logger.Global()).ConfigureFromFlags()
	}
	once.Do(init)
}

// V23Init initializes the runtime and sets up some convenient infrastructure for tests:
// - Sets a freshly created principal (with a single self-signed blessing) on it.
// - Creates a mounttable and sets the namespace roots appropriately
// Both steps are skipped if this function is invoked from a process run
// using the modules package.
func V23Init() (*context.T, v23.Shutdown) {
	moduleProcess := os.Getenv("V23_SHELL_HELPER_PROCESS_ENTRY_POINT") != ""
	return V23InitWithParams(InitParams{
		CreatePrincipal:  !moduleProcess,
		CreateMounttable: !moduleProcess,
	})
}

// Params contains parameters for tests that need to control what happens during
// init carefully.
type InitParams struct {
	CreateMounttable bool // CreateMounttable creates a new mounttable.
	CreatePrincipal  bool // CreatePrincipal creates a new principal with self-signed blessing.
}

// V23InitWithParams initializes the runtime and returns a new context and shutdown function.
// Specific aspects of initialization can be controlled via the params struct.
func V23InitWithParams(params InitParams) (*context.T, v23.Shutdown) {
	ctx, shutdown := v23.Init()
	if params.CreatePrincipal {
		var err error
		if ctx, err = v23.WithPrincipal(ctx, testutil.NewPrincipal(TestBlessing)); err != nil {
			panic(err)
		}
	}
	if params.CreateMounttable {
		disp, err := mounttablelib.NewMountTableDispatcher("", "", "mounttable")
		if err != nil {
			panic(err)
		}
		s, err := xrpc.NewDispatchingServer(ctx, "", disp, options.ServesMountTable(true))
		if err != nil {
			panic(err)
		}
		ns := v23.GetNamespace(ctx)
		ns.SetRoots(s.Status().Endpoints[0].Name())
		ns.CacheCtl(naming.DisableCache(true))
	}
	return ctx, shutdown
}

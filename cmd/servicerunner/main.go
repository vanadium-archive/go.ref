// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build wspr
//
// We restrict to a special build-tag since it's required by wsprlib.
//
// Manually run the following to generate the doc.go file.  This isn't a
// go:generate comment, since generate also needs to be run with -tags=wspr,
// which is troublesome for presubmit tests.
//
// cd $JIRI_ROOT/release/go/src && go run v.io/x/lib/cmdline/testdata/gendoc.go -tags=wspr v.io/x/ref/cmd/servicerunner -help

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"v.io/v23"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/cmdline"
	"v.io/x/lib/set"
	"v.io/x/ref"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/identity/identitylib"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/services/wspr/wsprlib"
	"v.io/x/ref/services/xproxy/xproxy"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
)

var (
	port   int
	identd string
)

func init() {
	wsprlib.OverrideCaveatValidation()
	cmdServiceRunner.Flags.IntVar(&port, "port", 8124, "Port for wspr to listen on.")
	cmdServiceRunner.Flags.StringVar(&identd, "identd", "", "Name of wspr identd server.")
}

func main() {
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdServiceRunner)
}

var cmdServiceRunner = &cmdline.Command{
	Runner: cmdline.RunnerFunc(run),
	Name:   "servicerunner",
	Short:  "Runs several services, including the mounttable, proxy and wspr.",
	Long: `
Command servicerunner runs several Vanadium services, including the mounttable,
proxy and wspr.  It prints a JSON map with their vars to stdout (as a single
line), then waits forever.
`,
}

var rootMT = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	mt, err := mounttablelib.NewMountTableDispatcher(ctx, "", "", "mounttable")
	if err != nil {
		return fmt.Errorf("mounttablelib.NewMountTableDispatcher failed: %s", err)
	}
	ctx, server, err := v23.WithNewDispatchingServer(ctx, "", mt, options.ServesMountTable(true))
	if err != nil {
		return fmt.Errorf("root failed: %v", err)
	}
	fmt.Fprintf(env.Stdout, "PID=%d\n", os.Getpid())
	for _, ep := range server.Status().Endpoints {
		fmt.Fprintf(env.Stdout, "MT_NAME=%s\n", ep.Name())
	}
	modules.WaitForEOF(env.Stdin)
	return nil
}, "rootMT")

// updateVars captures the vars from the given Handle's stdout and adds them to
// the given vars map, overwriting existing entries.
func updateVars(h modules.Handle, vars map[string]string, varNames ...string) error {
	varsToAdd := set.StringBool.FromSlice(varNames)
	numLeft := len(varsToAdd)

	s := expect.NewSession(nil, h.Stdout(), 30*time.Second)
	for {
		l := s.ReadLine()
		if err := s.OriginalError(); err != nil {
			return err // EOF or otherwise
		}
		parts := strings.Split(l, "=")
		if len(parts) != 2 {
			return fmt.Errorf("Unexpected line: %s", l)
		}
		if varsToAdd[parts[0]] {
			numLeft--
			vars[parts[0]] = parts[1]
			if numLeft == 0 {
				break
			}
		}
	}
	return nil
}

func run(env *cmdline.Env, args []string) error {
	// The dispatch to modules children must occur after the call to cmdline.Main
	// (which calls cmdline.Parse), so that servicerunner flags are registered on
	// the global flag.CommandLine.
	if modules.IsChildProcess() {
		return modules.Dispatch()
	}

	// We must wait until after we've dispatched to children before calling
	// v23.Init, otherwise we'll end up initializing twice.
	ctx, shutdown := v23.Init()
	defer shutdown()

	vars := map[string]string{}
	sh, err := modules.NewShell(ctx, nil, false, nil)
	if err != nil {
		panic(fmt.Sprintf("modules.NewShell: %s", err))
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	h, err := sh.Start(nil, rootMT, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0")
	if err != nil {
		return err
	}
	if err := updateVars(h, vars, "MT_NAME"); err != nil {
		return err
	}

	// Set ref.EnvNamespacePrefix env var, consumed downstream.
	sh.SetVar(ref.EnvNamespacePrefix, vars["MT_NAME"])
	v23.GetNamespace(ctx).SetRoots(vars["MT_NAME"])

	lspec := v23.GetListenSpec(ctx)
	lspec.Addrs = rpc.ListenAddrs{{"ws", "127.0.0.1:0"}}
	ctx = v23.WithListenSpec(ctx, lspec)
	var (
		proxyShutdown func()
		proxyEndpoint naming.Endpoint
	)
	if ref.RPCTransitionState() == ref.XServers {
		proxy, err := xproxy.New(ctx, "test/proxy", security.AllowEveryone())
		if err != nil {
			return err
		}
		proxyEndpoint = proxy.ListeningEndpoints()[0]
		proxyShutdown = func() {}
	} else {
		proxyShutdown, proxyEndpoint, err = generic.NewProxy(ctx, lspec, security.AllowEveryone(), "test/proxy")
		if err != nil {
			return err
		}
	}
	defer proxyShutdown()
	vars["PROXY_NAME"] = proxyEndpoint.Name()

	h, err = sh.Start(nil, wsprd, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0", "--v23.proxy=test/proxy", "--identd=test/identd")
	if err != nil {
		return err
	}
	if err := updateVars(h, vars, "WSPR_ADDR"); err != nil {
		return err
	}

	h, err = sh.Start(nil, identitylib.TestIdentityd, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0", "--http-addr=localhost:0")
	if err != nil {
		return err
	}
	if err := updateVars(h, vars, "TEST_IDENTITYD_NAME", "TEST_IDENTITYD_HTTP_ADDR"); err != nil {
		return err
	}

	bytes, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	fmt.Println(string(bytes))

	<-signals.ShutdownOnSignals(ctx)
	return nil
}

var wsprd = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	l := v23.GetListenSpec(ctx)
	proxy := wsprlib.NewWSPR(ctx, port, &l, identd, nil)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	fmt.Fprintf(env.Stdout, "WSPR_ADDR=%s\n", addr)
	modules.WaitForEOF(env.Stdin)
	return nil
}, "wsprd")

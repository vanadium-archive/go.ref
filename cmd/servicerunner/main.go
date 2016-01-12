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

	"v.io/v23"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/cmdline"
	"v.io/x/lib/gosh"
	"v.io/x/lib/set"
	"v.io/x/ref"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23test"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/identity/identitylib"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/services/wspr/wsprlib"
	"v.io/x/ref/services/xproxy/xproxy"
	"v.io/x/ref/test/expect"
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

// TODO(sadovsky): Switch to using v23test.Shell.StartRootMountTable.
var rootMT = gosh.Register("rootMT", func() error {
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
	fmt.Printf("PID=%d\n", os.Getpid())
	for _, ep := range server.Status().Endpoints {
		fmt.Printf("MT_NAME=%s\n", ep.Name())
	}
	<-signals.ShutdownOnSignals(ctx)
	return nil
})

// updateVars captures the vars from the given session and adds them to the
// given vars map, overwriting existing entries.
// TODO(sadovsky): Switch to gosh.SendVars/AwaitVars.
func updateVars(s *expect.Session, vars map[string]string, varNames ...string) error {
	varsToAdd := set.StringBool.FromSlice(varNames)
	numLeft := len(varsToAdd)

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
	// gosh.InitMain must occur after the call to cmdline.Main (which calls
	// cmdline.Parse) so that servicerunner flags are registered on the global
	// flag.CommandLine.
	gosh.InitMain()

	sh := v23test.NewShell(nil, v23test.Opts{})
	defer sh.Cleanup()
	ctx := sh.Ctx

	// FIXME
	fmt.Fprintf(os.Stderr, "\n\n@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n")
	fmt.Fprintf(os.Stderr, v23.GetPrincipal(ctx).BlessingStore().DebugString())
	fmt.Fprintf(os.Stderr, "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n\n")

	vars := map[string]string{}
	c := sh.Fn(rootMT)
	c.Args = append(c.Args, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0")
	c.Start()
	if err := updateVars(c.S, vars, "MT_NAME"); err != nil {
		return err
	}

	// Set ref.EnvNamespacePrefix env var, consumed downstream.
	sh.Vars[ref.EnvNamespacePrefix] = vars["MT_NAME"]
	v23.GetNamespace(ctx).SetRoots(vars["MT_NAME"])

	lspec := v23.GetListenSpec(ctx)
	lspec.Addrs = rpc.ListenAddrs{{"ws", "127.0.0.1:0"}}
	ctx = v23.WithListenSpec(ctx, lspec)
	proxy, err := xproxy.New(ctx, "test/proxy", security.AllowEveryone())
	if err != nil {
		return err
	}
	proxyEndpoint := proxy.ListeningEndpoints()[0]
	vars["PROXY_NAME"] = proxyEndpoint.Name()

	c = sh.Fn(wsprd)
	c.Args = append(c.Args, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0", "--v23.proxy=test/proxy", "--identd=test/identd")
	c.Start()
	if err := updateVars(c.S, vars, "WSPR_ADDR"); err != nil {
		return err
	}

	c = sh.Fn(identitylib.TestIdentityd)
	c.Args = append(c.Args, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0", "--http-addr=localhost:0")
	c.Start()
	if err := updateVars(c.S, vars, "TEST_IDENTITYD_NAME", "TEST_IDENTITYD_HTTP_ADDR"); err != nil {
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

var wsprd = gosh.Register("wsprd", func() error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	l := v23.GetListenSpec(ctx)
	proxy := wsprlib.NewWSPR(ctx, port, &l, identd, nil)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	fmt.Printf("WSPR_ADDR=%s\n", addr)
	<-signals.ShutdownOnSignals(ctx)
	return nil
})

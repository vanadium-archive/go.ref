// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build wspr
//
// We restrict to a special build-tag since it's required by wsprlib.

// Command servicerunner runs several Vanadium services, including the
// mounttable, proxy and wspr.  It prints a JSON map with their vars to stdout
// (as a single line), then waits forever.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"v.io/v23"
	"v.io/v23/options"
	"v.io/v23/rpc"

	"v.io/x/ref/envvar"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/profiles"
	"v.io/x/ref/services/identity/identitylib"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
)

func panicOnError(err error) {
	if err != nil {
		panic(err)
	}
}

func init() {
	modules.RegisterChild("rootMT", ``, rootMT)
}

func rootMT(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	lspec := v23.GetListenSpec(ctx)
	server, err := v23.NewServer(ctx, options.ServesMountTable(true))
	if err != nil {
		return fmt.Errorf("root failed: %v", err)
	}
	mt, err := mounttablelib.NewMountTableDispatcher("", "mounttable")
	if err != nil {
		return fmt.Errorf("mounttablelib.NewMountTableDispatcher failed: %s", err)
	}
	eps, err := server.Listen(lspec)
	if err != nil {
		return fmt.Errorf("server.Listen failed: %s", err)
	}
	if err := server.ServeDispatcher("", mt); err != nil {
		return fmt.Errorf("root failed: %s", err)
	}
	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	for _, ep := range eps {
		fmt.Fprintf(stdout, "MT_NAME=%s\n", ep.Name())
	}
	modules.WaitForEOF(stdin)
	return nil
}

// updateVars captures the vars from the given Handle's stdout and adds them to
// the given vars map, overwriting existing entries.
func updateVars(h modules.Handle, vars map[string]string, varNames ...string) error {
	varsToAdd := map[string]bool{}
	for _, v := range varNames {
		varsToAdd[v] = true
	}
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
		if _, ok := varsToAdd[parts[0]]; ok {
			numLeft--
			vars[parts[0]] = parts[1]
			if numLeft == 0 {
				break
			}
		}
	}
	return nil
}

func main() {
	if modules.IsModulesChildProcess() {
		panicOnError(modules.Dispatch())
		return
	}

	ctx, shutdown := v23.Init()
	defer shutdown()

	vars := map[string]string{}

	sh, err := modules.NewShell(ctx, nil, false, nil)
	if err != nil {
		panic(fmt.Sprintf("modules.NewShell: %s", err))
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	h, err := sh.Start("rootMT", nil, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0")
	panicOnError(err)
	panicOnError(updateVars(h, vars, "MT_NAME"))

	// Set envvar.NamespacePrefix env var, consumed downstream.
	sh.SetVar(envvar.NamespacePrefix, vars["MT_NAME"])
	v23.GetNamespace(ctx).SetRoots(vars["MT_NAME"])

	lspec := v23.GetListenSpec(ctx)
	lspec.Addrs = rpc.ListenAddrs{{"ws", "127.0.0.1:0"}}
	proxyShutdown, proxyEndpoint, err := profiles.NewProxy(ctx, lspec, "test/proxy")
	defer proxyShutdown()
	vars["PROXY_NAME"] = proxyEndpoint.Name()

	h, err = sh.Start(WSPRDCommand, nil, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0", "--v23.proxy=test/proxy", "--identd=test/identd")
	panicOnError(err)
	panicOnError(updateVars(h, vars, "WSPR_ADDR"))

	h, err = sh.Start(identitylib.TestIdentitydCommand, nil, "--v23.tcp.protocol=ws", "--v23.tcp.address=127.0.0.1:0", "--v23.proxy=test/proxy", "--http-addr=localhost:0")
	panicOnError(err)
	panicOnError(updateVars(h, vars, "TEST_IDENTITYD_NAME", "TEST_IDENTITYD_HTTP_ADDR"))

	bytes, err := json.Marshal(vars)
	panicOnError(err)
	fmt.Println(string(bytes))

	<-signals.ShutdownOnSignals(ctx)
}

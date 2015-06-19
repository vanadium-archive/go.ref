// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc_test

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"

	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/xrpc"
	"v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/runtime/internal"
	"v.io/x/ref/runtime/internal/lib/appcycle"
	inaming "v.io/x/ref/runtime/internal/naming"
	irpc "v.io/x/ref/runtime/internal/rpc"
	grt "v.io/x/ref/runtime/internal/rt"
	"v.io/x/ref/services/mounttable/mounttablelib"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
)

var commonFlags *flags.Flags

func init() {
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime)
	if err := internal.ParseFlags(commonFlags); err != nil {
		panic(err)
	}
}

func setupRuntime() {
	ac := appcycle.New()

	listenSpec := rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}

	rootctx, rootcancel := context.RootContext()
	ctx, cancel := context.WithCancel(rootctx)
	runtime, ctx, sd, err := grt.Init(ctx,
		ac,
		nil,
		&listenSpec,
		nil,
		"",
		commonFlags.RuntimeFlags(),
		nil)
	if err != nil {
		panic(err)
	}
	shutdown := func() {
		ac.Shutdown()
		cancel()
		sd()
		rootcancel()
	}
	fake.InjectRuntime(runtime, ctx, shutdown)
}

var rootMT = modules.Register(func(env *modules.Env, args ...string) error {
	setupRuntime()
	ctx, shutdown := v23.Init()
	defer shutdown()

	mp := ""
	mt, err := mounttablelib.NewMountTableDispatcher("", "", "mounttable")
	if err != nil {
		return fmt.Errorf("mounttablelib.NewMountTableDispatcher failed: %s", err)
	}
	server, err := xrpc.NewDispatchingServer(ctx, mp, mt, options.ServesMountTable(true))
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

func startMT(t *testing.T, sh *modules.Shell) string {
	h, err := sh.Start(nil, rootMT)
	if err != nil {
		t.Fatalf("unexpected error for root mt: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.ExpectVar("PID")
	return s.ExpectVar("MT_NAME")
}

func TestResolveToEndpoint(t *testing.T) {
	setupRuntime()
	ctx, shutdown := v23.Init()
	defer shutdown()
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("modules.NewShell failed: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	root := startMT(t, sh)

	ns := v23.GetNamespace(ctx)
	ns.SetRoots(root)

	proxyEp, _ := inaming.NewEndpoint("proxy.v.io:123")
	proxyEpStr := proxyEp.String()
	proxyAddr := naming.JoinAddressName(proxyEpStr, "")
	if err := ns.Mount(ctx, "proxy", proxyAddr, time.Hour); err != nil {
		t.Fatalf("ns.Mount failed: %s", err)
	}

	server, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("runtime.NewServer failed: %s", err)
	}

	notfound := fmt.Errorf("not found")
	testcases := []struct {
		address string
		result  string
		err     error
	}{
		{"/proxy.v.io:123", proxyEpStr, nil},
		{"proxy.v.io:123", "", notfound},
		{"proxy", proxyEpStr, nil},
		{naming.JoinAddressName(root, "proxy"), proxyEpStr, nil},
		{proxyAddr, proxyEpStr, nil},
		{proxyEpStr, "", notfound},
		{"unknown", "", notfound},
	}
	for _, tc := range testcases {
		result, err := irpc.InternalServerResolveToEndpoint(server, tc.address)
		if (err == nil) != (tc.err == nil) {
			t.Errorf("Unexpected err for %q. Got %v, expected %v", tc.address, err, tc.err)
		}
		if result != tc.result {
			t.Errorf("Unexpected result for %q. Got %q, expected %q", tc.address, result, tc.result)
		}
	}
	if t.Failed() {
		t.Logf("proxyEpStr: %v", proxyEpStr)
		t.Logf("proxyAddr: %v", proxyAddr)
	}
}

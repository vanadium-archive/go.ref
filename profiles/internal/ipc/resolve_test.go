package ipc_test

import (
	"flag"
	"fmt"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"

	"v.io/x/ref/lib/flags"
	"v.io/x/ref/profiles/fake"
	"v.io/x/ref/profiles/internal"
	iipc "v.io/x/ref/profiles/internal/ipc"
	"v.io/x/ref/profiles/internal/lib/appcycle"
	inaming "v.io/x/ref/profiles/internal/naming"
	grt "v.io/x/ref/profiles/internal/rt"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/modules/core"
)

var commonFlags *flags.Flags

func init() {
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime)
	if err := internal.ParseFlags(commonFlags); err != nil {
		panic(err)
	}

	ac := appcycle.New()

	listenSpec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}

	rootctx, rootcancel := context.RootContext()
	ctx, cancel := context.WithCancel(rootctx)
	runtime, ctx, sd, err := grt.Init(ctx,
		ac,
		nil,
		&listenSpec,
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

func startMT(t *testing.T, sh *modules.Shell) string {
	h, err := sh.Start(core.RootMTCommand, nil)
	if err != nil {
		t.Fatalf("unexpected error for root mt: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.ExpectVar("PID")
	return s.ExpectVar("MT_NAME")
}

func TestResolveToEndpoint(t *testing.T) {
	sh, err := modules.NewShell(nil, nil)
	if err != nil {
		t.Fatalf("modules.NewShell failed: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	root := startMT(t, sh)

	ctx, shutdown := test.InitForTest()
	defer shutdown()

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
		result, err := iipc.InternalServerResolveToEndpoint(server, tc.address)
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

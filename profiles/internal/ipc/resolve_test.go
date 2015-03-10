package ipc_test

import (
	"fmt"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/naming"

	"v.io/x/ref/lib/modules"
	"v.io/x/ref/lib/modules/core"
	"v.io/x/ref/lib/testutil"
	"v.io/x/ref/lib/testutil/expect"
	iipc "v.io/x/ref/profiles/internal/ipc"
	inaming "v.io/x/ref/profiles/internal/naming"
)

func startMT(t *testing.T, sh *modules.Shell) string {
	h, err := sh.Start(core.RootMTCommand, nil, "--veyron.tcp.address=127.0.0.1:0")
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

	ctx, shutdown := testutil.InitForTest()
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

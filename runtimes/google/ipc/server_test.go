package ipc

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
	imanager "veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/proxy"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/sectest"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	inaming "veyron.io/veyron/veyron/runtimes/google/naming"
	tnaming "veyron.io/veyron/veyron/runtimes/google/testing/mocks/naming"
)

// TestReconnect verifies that the client transparently re-establishes the
// connection to the server if the server dies and comes back (on the same
// endpoint).
func TestReconnect(t *testing.T) {
	b := createBundle(t, sectest.NewPrincipal("client"), nil, nil) // We only need the client from the bundle.
	defer b.cleanup(t)
	sh := modules.NewShell()
	defer sh.Cleanup(os.Stderr, os.Stderr)
	server, err := sh.Start("runServer", nil, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	session := expect.NewSession(t, server.Stdout(), time.Minute)
	addr := session.ReadLine()
	ep, err := inaming.NewEndpoint(addr)
	if err != nil {
		t.Fatalf("inaming.NewEndpoint(%q): %v", addr, err)
	}
	serverName := naming.JoinAddressName(ep.String(), "suffix")
	makeCall := func() (string, error) {
		ctx, _ := testContext().WithDeadline(time.Now().Add(5 * time.Second))
		call, err := b.client.StartCall(ctx, serverName, "Echo", []interface{}{"bratman"})
		if err != nil {
			return "", fmt.Errorf("START: %s", err)
		}
		var result string
		if err = call.Finish(&result); err != nil {
			return "", err
		}
		return result, nil
	}
	expected := `method:"Echo",suffix:"suffix",arg:"bratman"`
	if result, err := makeCall(); err != nil || result != expected {
		t.Errorf("Got (%q, %v) want (%q, nil)", result, err, expected)
	}
	// Kill the server, verify client can't talk to it anymore.
	server.Shutdown(nil, nil)
	if _, err := makeCall(); err == nil || (!strings.HasPrefix(err.Error(), "START") && !strings.Contains(err.Error(), "EOF")) {
		t.Fatalf(`Got (%v) want ("START: <err>" or "EOF") as server is down`, err)
	}

	// Resurrect the server with the same address, verify client
	// re-establishes the connection.
	server, err = sh.Start("runServer", nil, addr)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	session = expect.NewSession(t, server.Stdout(), time.Minute)
	defer server.Shutdown(nil, nil)
	session.Expect(addr)
	if result, err := makeCall(); err != nil || result != expected {
		t.Errorf("Got (%q, %v) want (%q, nil)", result, err, expected)
	}
}

type proxyHandle struct {
	ns      naming.Namespace
	process modules.Handle
	session *expect.Session
	mount   string
}

func (h *proxyHandle) Start(t *testing.T) error {
	sh := modules.NewShell()
	server, err := sh.Start("runProxy", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	h.process = server
	h.session = expect.NewSession(t, server.Stdout(), time.Minute)
	h.mount = h.session.ReadLine()
	if err := h.session.Error(); err != nil {
		return err
	}
	if err := h.ns.Mount(testContext(), "proxy", h.mount, time.Hour); err != nil {
		return err
	}
	return nil
}

func (h *proxyHandle) Stop() error {
	if h.process == nil {
		return nil
	}
	h.process.Shutdown(os.Stderr, os.Stderr)
	h.process = nil
	if len(h.mount) == 0 {
		return nil
	}
	return h.ns.Unmount(testContext(), "proxy", h.mount)
}

func TestProxyOnly(t *testing.T) {
	listenSpec := ipc.ListenSpec{Proxy: "proxy"}
	testProxy(t, listenSpec)
}

func TestProxy(t *testing.T) {
	listenSpec := ipc.ListenSpec{Protocol: "tcp", Address: "127.0.0.1:0", Proxy: "proxy"}
	testProxy(t, listenSpec)
}

func addrOnly(name string) string {
	addr, _ := naming.SplitAddressName(name)
	return addr
}

func testProxy(t *testing.T, spec ipc.ListenSpec) {
	sm := imanager.InternalNew(naming.FixedRoutingID(0x555555555))
	ns := tnaming.NewSimpleNamespace()
	client, err := InternalNewClient(sm, ns, vc.LocalPrincipal{sectest.NewPrincipal("client")})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := InternalNewServer(testContext(), sm, ns, nil, vc.LocalPrincipal{sectest.NewPrincipal("server")})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// If no address is specified then we'll only 'listen' via
	// the proxy.
	hasLocalListener := len(spec.Address) != 0

	name := "mountpoint/server/suffix"
	makeCall := func() (string, error) {
		ctx, _ := testContext().WithDeadline(time.Now().Add(5 * time.Second))
		// Let's fail fast so that the tests don't take as long to run.
		call, err := client.StartCall(ctx, name, "Echo", []interface{}{"batman"})
		if err != nil {
			// proxy is down, we should return here/.... prepend
			// the error with a well known string so that we can test for that.
			return "", fmt.Errorf("RESOLVE: %s", err)
		}
		var result string
		if err = call.Finish(&result); err != nil {
			return "", err
		}
		return result, nil
	}
	proxy := &proxyHandle{ns: ns}
	if err := proxy.Start(t); err != nil {
		t.Fatal(err)
	}
	defer proxy.Stop()
	addrs := verifyMount(t, ns, "proxy")
	if len(addrs) != 1 {
		t.Fatalf("failed to lookup proxy")
	}
	proxyEP := addrOnly(addrs[0])

	ep, err := server.Listen(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.ServeDispatcher("mountpoint/server", testServerDisp{&testServer{}}); err != nil {
		t.Fatal(err)
	}

	ch := make(chan struct{})
	// Proxy connections are started asynchronously, so we need to wait..
	waitfor := func(expect int) {
		for {
			addrs, _ := ns.Resolve(testContext(), name)
			if len(addrs) == expect {
				close(ch)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	proxiedEP, err := inaming.NewEndpoint(proxyEP)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	proxiedEP.RID = naming.FixedRoutingID(0x555555555)
	expectedEndpoints := []string{proxiedEP.String()}
	if hasLocalListener {
		expectedEndpoints = append(expectedEndpoints, ep.String())
	}

	// Proxy connetions are created asynchronously, so we wait for the
	// expected number of endpoints to appear for the specified service name.
	go waitfor(len(expectedEndpoints))
	select {
	case <-time.After(time.Minute):
		t.Fatalf("timedout waiting for two entries in the mount table")
	case <-ch:
	}

	got := []string{}
	for _, s := range verifyMount(t, ns, name) {
		got = append(got, addrOnly(s))
	}
	sort.Strings(got)
	sort.Strings(expectedEndpoints)
	if !reflect.DeepEqual(got, expectedEndpoints) {
		t.Errorf("got %v, want %v", got, expectedEndpoints)
	}

	if hasLocalListener {
		// Listen will publish both the local and proxied endpoint with the
		// mount table, given that we're trying to test the proxy, we remove
		// the local endpoint from the mount table entry!
		ns.Unmount(testContext(), "mountpoint/server", naming.JoinAddressName(ep.String(), ""))
	}

	// Proxied endpoint should be published and RPC should succeed (through proxy)
	const expected = `method:"Echo",suffix:"suffix",arg:"batman"`
	if result, err := makeCall(); result != expected || err != nil {
		t.Fatalf("Got (%v, %v) want (%v, nil)", result, err, expected)
	}
	// Proxy dies, calls should fail and the name should be unmounted.
	if err := proxy.Stop(); err != nil {
		t.Fatal(err)
	}
	if result, err := makeCall(); err == nil || (!strings.HasPrefix(err.Error(), "RESOLVE") && !strings.Contains(err.Error(), "EOF")) {
		t.Fatalf(`Got (%v, %v) want ("", "RESOLVE: <err>" or "EOF") as proxy is down`, result, err)
	}
	for {
		if _, err := ns.Resolve(testContext(), name); err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	verifyMountMissing(t, ns, name)

	// Proxy restarts, calls should eventually start succeeding.
	if err := proxy.Start(t); err != nil {
		t.Fatal(err)
	}

	for {
		if result, err := makeCall(); err == nil {
			if result != expected {
				t.Errorf("Got (%v, %v) want (%v, nil)", result, err, expected)
			}
			break
		}
	}
}

func runServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	mgr := imanager.InternalNew(naming.FixedRoutingID(0x1111111))
	ns := tnaming.NewSimpleNamespace()
	server, err := InternalNewServer(testContext(), mgr, ns, nil, vc.LocalPrincipal{sectest.NewPrincipal("server")})
	if err != nil {
		return fmt.Errorf("InternalNewServer failed: %v", err)
	}
	disp := testServerDisp{new(testServer)}
	if err := server.ServeDispatcher("server", disp); err != nil {
		return fmt.Errorf("server.Register failed: %v", err)
	}
	spec := listenSpec
	spec.Address = args[1]
	ep, err := server.Listen(spec)
	if err != nil {
		return fmt.Errorf("server.Listen failed: %v", err)
	}
	fmt.Fprintf(stdout, "%s\n", ep.Addr())
	// parent process should explicitly shut us down by closing stdin.
	modules.WaitForEOF(stdin)
	return nil
}

func runProxy(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return err
	}
	proxy, err := proxy.New(rid, sectest.NewPrincipal("proxy"), "tcp", "127.0.0.1:0", "")
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "/%s\n", proxy.Endpoint().String())
	// parent process should explicitly shut us down by closing stdin.
	modules.WaitForEOF(stdin)
	return nil
}

// Required by modules framework.
func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func init() {
	modules.RegisterChild("runServer", "[address]", runServer)
	modules.RegisterChild("runProxy", "", runProxy)
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package manager

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"

	inaming "v.io/x/ref/runtime/internal/naming"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/tcp"
	_ "v.io/x/ref/runtime/internal/rpc/protocols/ws"
	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/vc"
	"v.io/x/ref/runtime/internal/rpc/stream/vif"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"
)

// We write our own TestMain here instead of relying on jiri test generate because
// we need to set runtime.GOMAXPROCS.
func TestMain(m *testing.M) {
	// For GOMAXPROCS to remain at 1 in order to trigger a particular race
	// condition that occurs when closing the server; also, using 1 cpu
	// introduces less variance in the behavior of the test.
	runtime.GOMAXPROCS(1)
	modules.DispatchAndExitIfChild()
	os.Exit(m.Run())
}

func testSimpleFlow(t *testing.T, protocol string) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0x55555555))
	client := InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
	pclient := testutil.NewPrincipal("client")
	pclient2 := testutil.NewPrincipal("client2")
	pserver := testutil.NewPrincipal("server")
	ctx, _ = v23.WithPrincipal(ctx, pserver)

	ln, ep, err := server.Listen(ctx, protocol, "127.0.0.1:0", pserver.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}

	data := "the dark knight rises"
	var clientVC stream.VC
	var clientF1 stream.Flow
	go func() {
		var err error
		cctx, _ := v23.WithPrincipal(ctx, pclient)
		if clientVC, err = client.Dial(cctx, ep); err != nil {
			t.Errorf("Dial(%q) failed: %v", ep, err)
			return
		}
		if clientF1, err = clientVC.Connect(); err != nil {
			t.Errorf("Connect() failed: %v", err)
			return
		}
		if err := writeLine(clientF1, data); err != nil {
			t.Error(err)
		}
	}()
	serverF, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept failed: %v", err)
	}
	if got, err := readLine(serverF); got != data || err != nil {
		t.Errorf("Got (%q, %v), want (%q, nil)", got, err, data)
	}
	// By this point, the goroutine has passed the write call (or exited
	// early) since the read has gotten through.  Check if the goroutine
	// encountered any errors in creating the VC or flow and abort.
	if t.Failed() {
		return
	}
	defer clientF1.Close()

	ln.Close()

	// Writes on flows opened before the server listener was closed should
	// still succeed.
	data = "the dark knight goes to bed"
	go func() {
		if err := writeLine(clientF1, data); err != nil {
			t.Error(err)
		}
	}()
	if got, err := readLine(serverF); got != data || err != nil {
		t.Errorf("Got (%q, %v), want (%q, nil)", got, err, data)
	}

	// Opening a new flow on an existing VC will succeed initially, but
	// writes on the client end will eventually fail once the server has
	// stopped listening.
	//
	// It will require a round-trip to the server to notice the failure,
	// hence the client should write enough data to ensure that the Write
	// call will not return before a round-trip.
	//
	// The length of the data is taken to exceed the queue buffer size
	// (DefaultBytesBufferedPerFlow), the shared counters (MaxSharedBytes)
	// and the per-flow counters (DefaultBytesBufferedPerFlow) that are
	// given when the flow gets established.
	//
	// TODO(caprita): separate the constants for the queue buffer size and
	// the default number of counters to avoid confusion.
	lotsOfData := string(make([]byte, vc.DefaultBytesBufferedPerFlow*2+vc.MaxSharedBytes+1))
	clientF2, err := clientVC.Connect()
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer clientF2.Close()
	if err := writeLine(clientF2, lotsOfData); err == nil {
		t.Errorf("Should not be able to Dial or Write after the Listener is closed")
	}
	// Opening a new VC should fail fast. Note that we need to use a different
	// principal since the client doesn't expect a response from a server
	// when re-using VIF authentication.
	cctx, _ := v23.WithPrincipal(ctx, pclient2)
	if _, err := client.Dial(cctx, ep); err == nil {
		t.Errorf("Should not be able to Dial after listener is closed")
	}
}

func TestSimpleFlow(t *testing.T) {
	testSimpleFlow(t, "tcp")
}

func TestSimpleFlowWS(t *testing.T) {
	testSimpleFlow(t, "ws")
}

func TestConnectionTimeout(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	client := InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))

	ch := make(chan error)
	go func() {
		// 203.0.113.0 is TEST-NET-3 from RFC5737
		ep, _ := inaming.NewEndpoint(naming.FormatEndpoint("tcp", "203.0.113.10:80"))
		nctx, _ := v23.WithPrincipal(ctx, testutil.NewPrincipal("client"))
		_, err := client.Dial(nctx, ep, DialTimeout(time.Second))
		ch <- err
	}()

	select {
	case err := <-ch:
		if err == nil {
			t.Fatalf("expected an error")
		}
	case <-time.After(time.Minute):
		t.Fatalf("timedout")
	}
}

func testAuthenticatedByDefault(t *testing.T, protocol string) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	var (
		server = InternalNew(ctx, naming.FixedRoutingID(0x55555555))
		client = InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))

		clientPrincipal = testutil.NewPrincipal("client")
		serverPrincipal = testutil.NewPrincipal("server")
		clientKey       = clientPrincipal.PublicKey()
		serverBlessings = serverPrincipal.BlessingStore().Default()
		cctx, _         = v23.WithPrincipal(ctx, clientPrincipal)
		sctx, _         = v23.WithPrincipal(ctx, serverPrincipal)
	)

	ln, ep, err := server.Listen(sctx, protocol, "127.0.0.1:0", serverPrincipal.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}
	// And the server blessing should be in the endpoint.
	if got, want := ep.BlessingNames(), []string{"server"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Got blessings %v from endpoint, want %v", got, want)
	}

	errs := make(chan error)

	testAuth := func(tag string, flow stream.Flow, wantServer security.Blessings, wantClientKey security.PublicKey) {
		// Since the client's blessing is expected to be self-signed we only test
		// its public key
		gotServer := flow.RemoteBlessings()
		gotClientKey := flow.LocalBlessings().PublicKey()
		if tag == "server" {
			gotServer = flow.LocalBlessings()
			gotClientKey = flow.RemoteBlessings().PublicKey()
		}
		if !reflect.DeepEqual(gotServer, wantServer) || !reflect.DeepEqual(gotClientKey, wantClientKey) {
			errs <- fmt.Errorf("%s: Server: Got Blessings %q, want %q. Server: Got Blessings %q, want %q", tag, gotServer, wantServer, gotClientKey, wantClientKey)
			return
		}
		errs <- nil
	}

	go func() {
		flow, err := ln.Accept()
		if err != nil {
			errs <- err
			return
		}
		defer flow.Close()
		testAuth("server", flow, serverBlessings, clientKey)
	}()

	go func() {
		vc, err := client.Dial(cctx, ep)
		if err != nil {
			errs <- err
			return
		}
		flow, err := vc.Connect()
		if err != nil {
			errs <- err
			return
		}
		defer flow.Close()
		testAuth("client", flow, serverBlessings, clientKey)
	}()

	if err := <-errs; err != nil {
		t.Error(err)
	}
	if err := <-errs; err != nil {
		t.Error(err)
	}
}

func TestAuthenticatedByDefault(t *testing.T) {
	testAuthenticatedByDefault(t, "tcp")
}

func TestAuthenticatedByDefaultWS(t *testing.T) {
	testAuthenticatedByDefault(t, "ws")
}

func numListeners(m stream.Manager) int   { return len(m.(*manager).listeners) }
func debugString(m stream.Manager) string { return m.(*manager).DebugString() }
func numVIFs(m stream.Manager) int        { return len(m.(*manager).vifs.List()) }

func TestListenEndpoints(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0xcafe))
	principal := testutil.NewPrincipal("test")
	ctx, _ = v23.WithPrincipal(ctx, principal)
	blessings := principal.BlessingStore().Default()
	ln1, ep1, err1 := server.Listen(ctx, "tcp", "127.0.0.1:0", blessings)
	ln2, ep2, err2 := server.Listen(ctx, "tcp", "127.0.0.1:0", blessings)
	// Since "127.0.0.1:0" was used as the network address, a random port will be
	// assigned in each case. The endpoint should include that random port.
	if err1 != nil {
		t.Error(err1)
	}
	if err2 != nil {
		t.Error(err2)
	}
	if ep1.String() == ep2.String() {
		t.Errorf("Both listeners got the same endpoint: %q", ep1)
	}
	if n, expect := numListeners(server), 2; n != expect {
		t.Errorf("expecting %d listeners, got %d for %s", n, expect, debugString(server))
	}
	ln1.Close()
	if n, expect := numListeners(server), 1; n != expect {
		t.Errorf("expecting %d listeners, got %d for %s", n, expect, debugString(server))
	}
	ln2.Close()
	if n, expect := numListeners(server), 0; n != expect {
		t.Errorf("expecting %d listeners, got %d for %s", n, expect, debugString(server))
	}
}

func acceptLoop(wg *sync.WaitGroup, ln stream.Listener) {
	for {
		f, err := ln.Accept()
		if err != nil {
			break
		}
		f.Close()
	}
	wg.Done()
}

func TestCloseListener(t *testing.T) {
	testCloseListener(t, "tcp")
}

func TestCloseListenerWS(t *testing.T) {
	testCloseListener(t, "ws")
}

func testCloseListener(t *testing.T, protocol string) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0x5e97e9))
	pclient := testutil.NewPrincipal("client")
	pserver := testutil.NewPrincipal("server")
	cctx, _ := v23.WithPrincipal(ctx, pclient)
	sctx, _ := v23.WithPrincipal(ctx, pserver)
	blessings := pserver.BlessingStore().Default()
	ln, ep, err := server.Listen(sctx, protocol, "127.0.0.1:0", blessings)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	// Server will just listen for flows and close them.
	go acceptLoop(&wg, ln)
	client := InternalNew(ctx, naming.FixedRoutingID(0xc1e41))
	if _, err = client.Dial(cctx, ep); err != nil {
		t.Fatal(err)
	}
	ln.Close()
	client = InternalNew(ctx, naming.FixedRoutingID(0xc1e42))
	if _, err := client.Dial(cctx, ep); err == nil {
		t.Errorf("client.Dial(%q) should have failed", ep)
	}
	time.Sleep(time.Second)
	wg.Wait()
}

func TestShutdown(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0x5e97e9))
	principal := testutil.NewPrincipal("test")
	ctx, _ = v23.WithPrincipal(ctx, principal)
	blessings := principal.BlessingStore().Default()
	ln, _, err := server.Listen(ctx, "tcp", "127.0.0.1:0", blessings)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	// Server will just listen for flows and close them.
	go acceptLoop(&wg, ln)
	if n, expect := numListeners(server), 1; n != expect {
		t.Errorf("expecting %d listeners, got %d for %s", n, expect, debugString(server))
	}
	server.Shutdown()
	if _, _, err := server.Listen(ctx, "tcp", "127.0.0.1:0", blessings); err == nil {
		t.Error("server should have shut down")
	}
	if n, expect := numListeners(server), 0; n != expect {
		t.Errorf("expecting %d listeners, got %d for %s", n, expect, debugString(server))
	}
	wg.Wait()
	fmt.Fprintf(os.Stderr, "DONE\n")
}

func TestShutdownEndpoint(t *testing.T) {
	testShutdownEndpoint(t, "tcp")
}

func TestShutdownEndpointWS(t *testing.T) {
	testShutdownEndpoint(t, "ws")
}

func testShutdownEndpoint(t *testing.T, protocol string) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0x55555555))
	client := InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
	principal := testutil.NewPrincipal("test")
	ctx, _ = v23.WithPrincipal(ctx, principal)

	ln, ep, err := server.Listen(ctx, protocol, "127.0.0.1:0", principal.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	// Server will just listen for flows and close them.
	go acceptLoop(&wg, ln)

	cctx, _ := v23.WithPrincipal(ctx, testutil.NewPrincipal("client"))
	vc, err := client.Dial(cctx, ep)
	if err != nil {
		t.Fatal(err)
	}
	if f, err := vc.Connect(); f == nil || err != nil {
		t.Errorf("vc.Connect failed: (%v, %v)", f, err)
	}
	client.ShutdownEndpoint(ep)
	if f, err := vc.Connect(); f != nil || err == nil {
		t.Errorf("vc.Connect unexpectedly succeeded: (%v, %v)", f, err)
	}
	ln.Close()
	wg.Wait()
}

func TestStartTimeout(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	const (
		startTime = 5 * time.Millisecond
	)

	var (
		server  = InternalNew(ctx, naming.FixedRoutingID(0x55555555))
		pserver = testutil.NewPrincipal("server")
		lopts   = []stream.ListenerOpt{vc.StartTimeout{Duration: startTime}}
	)
	defer server.Shutdown()

	sctx, _ := v23.WithPrincipal(ctx, pserver)

	// Pause the start timers.
	triggerTimers := vif.SetFakeTimers()

	ln, ep, err := server.Listen(sctx, "tcp", "127.0.0.1:0", pserver.BlessingStore().Default(), lopts...)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			_, err := ln.Accept()
			if err != nil {
				return
			}
		}
	}()
	// Arrange for the above goroutine to exit when the test finishes.
	defer ln.Close()

	conn, err := net.Dial(ep.Addr().Network(), ep.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial failed: %v", err)
	}
	defer conn.Close()

	// Trigger the start timers.
	triggerTimers()

	// No VC is opened. The VIF should be closed after start timeout.
	for range time.Tick(startTime) {
		if numVIFs(server) == 0 {
			break
		}
	}
}

func testIdleTimeout(t *testing.T, testServer bool) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	const (
		idleTime = 10 * time.Millisecond
		// We use a long wait time here since it takes some time to handle VC close
		// especially in race testing.
		waitTime = 150 * time.Millisecond
	)

	var (
		server  = InternalNew(ctx, naming.FixedRoutingID(0x55555555))
		client  = InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
		pclient = testutil.NewPrincipal("client")
		pserver = testutil.NewPrincipal("server")
		cctx, _ = v23.WithPrincipal(ctx, pclient)
		sctx, _ = v23.WithPrincipal(ctx, pserver)

		opts  []stream.VCOpt
		lopts []stream.ListenerOpt
	)

	if testServer {
		lopts = []stream.ListenerOpt{vc.IdleTimeout{Duration: idleTime}}
	} else {
		opts = []stream.VCOpt{vc.IdleTimeout{Duration: idleTime}}
	}

	// Pause the idle timers.
	triggerTimers := vif.SetFakeTimers()

	ln, ep, err := server.Listen(sctx, "tcp", "127.0.0.1:0", pserver.BlessingStore().Default(), lopts...)
	if err != nil {
		t.Fatal(err)
	}
	errch := make(chan error)
	done := make(chan struct{})
	go func() {
		for {
			_, err := ln.Accept()
			select {
			case <-done:
				return
			case errch <- err:
			}
		}
	}()
	// Arrange for the above goroutine to exit when the test finishes.
	defer func() { ln.Close(); close(done) }()

	vc, err := client.Dial(cctx, ep, opts...)
	if err != nil {
		t.Fatalf("client.Dial(%q) failed: %v", ep, err)
	}
	f, err := vc.Connect()
	if f == nil || err != nil {
		t.Fatalf("vc.Connect failed: (%v, %v)", f, err)
	}
	// Wait until the server accepts the flow or fails.
	if err = <-errch; err != nil {
		t.Fatalf("ln.Accept failed: %v", err)
	}

	// Trigger the idle timers.
	triggerTimers()

	// One active flow. The VIF should be kept open.
	time.Sleep(waitTime)
	if n := numVIFs(client); n != 1 {
		t.Errorf("Client has %d VIFs; want 1\n%v", n, debugString(client))
	}
	if n := numVIFs(server); n != 1 {
		t.Errorf("Server has %d VIFs; want 1\n%v", n, debugString(server))
	}

	f.Close()

	// The flow has been closed. The VIF should be closed after idle timeout.
	for range time.Tick(idleTime) {
		if numVIFs(client) == 0 && numVIFs(server) == 0 {
			break
		}
	}
}

func TestIdleTimeout(t *testing.T)       { testIdleTimeout(t, false) }
func TestIdleTimeoutServer(t *testing.T) { testIdleTimeout(t, true) }

/* TLS + resumption + channel bindings is broken: <https://secure-resumption.com/#channelbindings>.
func TestSessionTicketCache(t *testing.T) {
	server := InternalNew(naming.FixedRoutingID(0x55555555))
	_, ep, err := server.Listen("tcp", "127.0.0.1:0", testutil.NewPrincipal("server"))
	if err != nil {
		t.Fatal(err)
	}

	client := InternalNew(naming.FixedRoutingID(0xcccccccc))
	if _, err = client.Dial(ep, testutil.NewPrincipal("TestSessionTicketCacheClient")); err != nil {
		t.Fatalf("Dial(%q) failed: %v", ep, err)
	}

	if _, ok := client.(*manager).sessionCache.Get(ep.String()); !ok {
		t.Fatalf("SessionTicket from TLS handshake not cached")
	}
}
*/

func testMultipleVCs(t *testing.T, protocol string) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0x55555555))
	client := InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
	principal := testutil.NewPrincipal("test")
	sctx, _ := v23.WithPrincipal(ctx, principal)

	const nVCs = 2
	const data = "bugs bunny"

	// Have the server read from each flow and write to rchan.
	rchan := make(chan string)
	ln, ep, err := server.Listen(sctx, protocol, "127.0.0.1:0", principal.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}

	read := func(flow stream.Flow, c chan string) {
		var buf bytes.Buffer
		var tmp [1024]byte
		for {
			n, err := flow.Read(tmp[:])
			buf.Write(tmp[:n])
			if err == io.EOF {
				c <- buf.String()
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
		}
	}
	go func() {
		for i := 0; i < nVCs; i++ {
			flow, err := ln.Accept()
			if err != nil {
				t.Error(err)
				rchan <- ""
				continue
			}
			go read(flow, rchan)
		}
	}()

	// Have the client establish nVCs and a flow on each.
	var vcs [nVCs]stream.VC
	for i := 0; i < nVCs; i++ {
		var err error
		pclient := testutil.NewPrincipal("client")
		cctx, _ := v23.WithPrincipal(ctx, pclient)
		vcs[i], err = client.Dial(cctx, ep)
		if err != nil {
			t.Fatal(err)
		}
	}
	write := func(vc stream.VC) {
		if err != nil {
			ln.Close()
			t.Error(err)
			return
		}
		flow, err := vc.Connect()
		if err != nil {
			ln.Close()
			t.Error(err)
			return
		}
		defer flow.Close()
		if _, err := flow.Write([]byte(data)); err != nil {
			ln.Close()
			t.Error(err)
			return
		}
	}
	for _, vc := range vcs {
		go write(vc)
	}
	for i := 0; i < nVCs; i++ {
		if got := <-rchan; got != data {
			t.Errorf("Got %q want %q", got, data)
		}
	}
}

func TestMultipleVCs(t *testing.T) {
	testMultipleVCs(t, "tcp")
}

func TestMultipleVCsWS(t *testing.T) {
	testMultipleVCs(t, "ws")
}

func TestAddressResolution(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0x55555555))
	client := InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
	principal := testutil.NewPrincipal("test")
	sctx, _ := v23.WithPrincipal(ctx, principal)

	// Using "tcp4" instead of "tcp" because the latter can end up with IPv6
	// addresses and our Google Compute Engine integration test machines cannot
	// resolve IPv6 addresses.
	// As of April 2014, https://developers.google.com/compute/docs/networking
	// said that IPv6 is not yet supported.
	ln, ep, err := server.Listen(sctx, "tcp4", "127.0.0.1:0", principal.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go acceptLoop(&wg, ln)

	// We'd like an endpoint that contains an address that's different than the
	// one used for the connection. In practice this is awkward to achieve since
	// we don't want to listen on ":0" since that will annoy firewalls. Instead we
	// create a endpoint with "localhost", which will result in an endpoint that
	// doesn't contain 127.0.0.1.
	_, port, _ := net.SplitHostPort(ep.Addr().String())
	nep := &inaming.Endpoint{
		Protocol: ep.Addr().Network(),
		Address:  net.JoinHostPort("localhost", port),
		RID:      ep.RoutingID(),
	}

	// Dial multiple VCs
	for i := 0; i < 2; i++ {
		pclient := testutil.NewPrincipal("client")
		cctx, _ := v23.WithPrincipal(ctx, pclient)
		if _, err = client.Dial(cctx, nep); err != nil {
			t.Fatalf("Dial #%d failed: %v", i, err)
		}
	}
	// They should all be on the same VIF.
	if n := numVIFs(client); n != 1 {
		t.Errorf("Client has %d VIFs, want 1\n%v", n, debugString(client))
	}
	ln.Close()
	wg.Wait()
	// TODO(ashankar): While a VIF can be re-used to Dial from the server
	// to the client, currently there is no way to have the client "listen"
	// on the same VIF. It can listen on a VC for new flows, but it cannot
	// listen on an established VIF for new VCs. Figure this out?
}

func TestServerRestartDuringClientLifetime(t *testing.T) {
	testServerRestartDuringClientLifetime(t, "tcp")
}

func TestServerRestartDuringClientLifetimeWS(t *testing.T) {
	testServerRestartDuringClientLifetime(t, "ws")
}

func testServerRestartDuringClientLifetime(t *testing.T, protocol string) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	client := InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
	pclient := testutil.NewPrincipal("client")
	pclient2 := testutil.NewPrincipal("client2")
	ctx1, _ := v23.WithPrincipal(ctx, pclient)
	ctx2, _ := v23.WithPrincipal(ctx, pclient2)

	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.Start(nil, runServer, protocol, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	epstr := expect.NewSession(t, h.Stdout(), time.Minute).ExpectVar("ENDPOINT")
	ep, err := inaming.NewEndpoint(epstr)
	if err != nil {
		t.Fatalf("inaming.NewEndpoint(%q): %v", epstr, err)
	}
	if _, err := client.Dial(ctx1, ep); err != nil {
		t.Fatal(err)
	}
	h.Shutdown(nil, os.Stderr)

	// A new VC cannot be created since the server is dead. Note that we need to
	// use a different principal since the client doesn't expect a response from
	// a server when re-using VIF authentication.
	if _, err := client.Dial(ctx2, ep); err == nil {
		t.Fatal("Expected client.Dial to fail since server is dead")
	}

	h, err = sh.Start(nil, runServer, protocol, ep.Addr().String())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// Restarting the server, listening on the same address as before
	ep2, err := inaming.NewEndpoint(expect.NewSession(t, h.Stdout(), time.Minute).ExpectVar("ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ep.Addr().String(), ep2.Addr().String(); got != want {
		t.Fatalf("Got %q, want %q", got, want)
	}
	if _, err := client.Dial(ctx1, ep2); err != nil {
		t.Fatal(err)
	}
}

var runServer = modules.Register(runServerFunc, "runServer")

func runServerFunc(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0x55555555))
	principal := testutil.NewPrincipal("test")
	ctx, _ = v23.WithPrincipal(ctx, principal)
	_, ep, err := server.Listen(ctx, args[0], args[1], principal.BlessingStore().Default())
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return err
	}
	fmt.Fprintf(env.Stdout, "ENDPOINT=%v\n", ep)
	// Live forever (till the process is explicitly killed)
	modules.WaitForEOF(env.Stdin)
	return nil
}

var runRLimitedServer = modules.Register(func(env *modules.Env, args ...string) error {
	var rlimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return err
	}
	rlimit.Cur = 9
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlimit); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return err
	}
	fmt.Fprintf(env.Stdout, "RLIMIT_NOFILE=%d\n", rlimit.Cur)
	return runServerFunc(env, args...)
}, "runRLimitedServer")

func readLine(f stream.Flow) (string, error) {
	var result bytes.Buffer
	var buf [5]byte
	for {
		n, err := f.Read(buf[:])
		result.Write(buf[:n])
		if err == io.EOF || buf[n-1] == '\n' {
			return strings.TrimRight(result.String(), "\n"), nil
		}
		if err != nil {
			return "", fmt.Errorf("Read returned (%d, %v)", n, err)
		}
	}
}

func writeLine(f stream.Flow, data string) error {
	data = data + "\n"
	if n, err := f.Write([]byte(data)); err != nil {
		return fmt.Errorf("Write returned (%d, %v)", n, err)
	}
	return nil
}

func TestRegistration(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	server := InternalNew(ctx, naming.FixedRoutingID(0x55555555))
	client := InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
	principal := testutil.NewPrincipal("server")
	blessings := principal.BlessingStore().Default()
	ctx, _ = v23.WithPrincipal(ctx, principal)

	dialer := func(_ *context.T, _, _ string, _ time.Duration) (net.Conn, error) {
		return nil, fmt.Errorf("tn.Dial")
	}
	resolver := func(_ *context.T, _, _ string) (string, string, error) {
		return "", "", fmt.Errorf("tn.Resolve")
	}
	listener := func(_ *context.T, _, _ string) (net.Listener, error) {
		return nil, fmt.Errorf("tn.Listen")
	}
	rpc.RegisterProtocol("tn", dialer, resolver, listener)

	_, _, err := server.Listen(ctx, "tnx", "127.0.0.1:0", blessings)
	if err == nil || !strings.Contains(err.Error(), "unknown network: tnx") {
		t.Fatalf("expected error is missing (%v)", err)
	}

	_, _, err = server.Listen(ctx, "tn", "127.0.0.1:0", blessings)
	if err == nil || !strings.Contains(err.Error(), "tn.Listen") {
		t.Fatalf("expected error is missing (%v)", err)
	}

	// Need a functional listener to test Dial.
	listener = func(_ *context.T, _, addr string) (net.Listener, error) {
		return net.Listen("tcp", addr)
	}

	if got, want := rpc.RegisterProtocol("tn", dialer, resolver, listener), true; got != want {
		t.Errorf("got %t, want %t", got, want)
	}

	_, ep, err := server.Listen(ctx, "tn", "127.0.0.1:0", blessings)
	if err != nil {
		t.Errorf("unexpected error %s", err)
	}

	cctx, _ := v23.WithPrincipal(ctx, testutil.NewPrincipal("client"))
	_, err = client.Dial(cctx, ep)
	if err == nil || !strings.Contains(err.Error(), "tn.Resolve") {
		t.Fatalf("expected error is missing (%v)", err)
	}
}

func TestBlessingNamesInEndpoint(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	var (
		p    = testutil.NewPrincipal("default")
		b, _ = p.BlessSelf("dev.v.io/users/foo@bar.com/devices/desktop/app/myapp")

		server = InternalNew(ctx, naming.FixedRoutingID(0x1))

		tests = []struct {
			principal     security.Principal
			blessings     security.Blessings
			blessingNames []string
			err           bool
		}{
			{
				// provided blessings should match returned output.
				principal:     p,
				blessings:     b,
				blessingNames: []string{"dev.v.io/users/foo@bar.com/devices/desktop/app/myapp"},
			},
			{
				// It is an error to provide a principal without providing blessings.
				principal: p,
				blessings: security.Blessings{},
				err:       true,
			},
			{
				// It is an error to provide inconsistent blessings and principal
				principal: testutil.NewPrincipal("random"),
				blessings: b,
				err:       true,
			},
		}
	)

	// p must recognize its own blessings!
	security.AddToRoots(p, b)
	for idx, test := range tests {
		sctx, _ := v23.WithPrincipal(ctx, test.principal)
		ln, ep, err := server.Listen(sctx, "tcp", "127.0.0.1:0", test.blessings)
		if (err != nil) != test.err {
			t.Errorf("test #%d: Got error %v, wanted error: %v", idx, err, test.err)
		}
		if err != nil {
			continue
		}
		ln.Close()
		got, want := ep.BlessingNames(), test.blessingNames
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("test #%d: Got %v, want %v", idx, got, want)
		}
	}
}

func TestVIFCleanupWhenFDLimitIsReached(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.Start(nil, runRLimitedServer, "--logtostderr=true", "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer h.CloseStdin()
	stdout := expect.NewSession(t, h.Stdout(), time.Minute)
	nfiles, err := strconv.Atoi(stdout.ExpectVar("RLIMIT_NOFILE"))
	if stdout.Error() != nil {
		t.Fatal(stdout.Error())
	}
	if err != nil {
		t.Fatal(err)
	}
	epstr := stdout.ExpectVar("ENDPOINT")
	if stdout.Error() != nil {
		t.Fatal(stdout.Error())
	}
	ep, err := inaming.NewEndpoint(epstr)
	if err != nil {
		t.Fatal(err)
	}
	// Different client processes (represented by different stream managers
	// in this test) should be able to make progress, even if the server
	// has reached its file descriptor limit.
	nattempts := 0
	for i := 0; i < 2*nfiles; i++ {
		client := InternalNew(ctx, naming.FixedRoutingID(uint64(i)))
		defer client.Shutdown()
		principal := testutil.NewPrincipal(fmt.Sprintf("client%d", i))
		cctx, _ := v23.WithPrincipal(ctx, principal)
		connected := false
		for !connected {
			nattempts++
			// If the client connection reached the server when it
			// was at its limit, it might fail.  However, this
			// failure will trigger the "kill connections" logic at
			// the server and eventually the client should succeed.
			vc, err := client.Dial(cctx, ep)
			if err != nil {
				continue
			}
			// Establish a flow to prevent the VC (and thus the
			// underlying VIF) from being garbage collected as an
			// "inactive" connection.
			flow, err := vc.Connect()
			if err != nil {
				continue
			}
			defer flow.Close()
			connected = true
		}
	}
	var stderr bytes.Buffer
	if err := h.Shutdown(nil, &stderr); err != nil {
		t.Logf("%s", stderr.String())
		t.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "11\n")
	if log := expect.NewSession(t, bytes.NewReader(stderr.Bytes()), time.Minute).ExpectSetEventuallyRE("listener.go.*Killing [1-9][0-9]* Conns"); len(log) == 0 {
		t.Errorf("Failed to find log message talking about killing Conns in:\n%v", stderr.String())
	}
	t.Logf("Server FD limit:%d", nfiles)
	t.Logf("Client connection attempts: %d", nattempts)
}

func TestConcurrentDials(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	// Concurrent Dials to the same network, address should only result in one VIF.
	server := InternalNew(ctx, naming.FixedRoutingID(0x55555555))
	client := InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
	principal := testutil.NewPrincipal("test")
	ctx, _ = v23.WithPrincipal(ctx, principal)

	// Using "tcp4" instead of "tcp" because the latter can end up with IPv6
	// addresses and our Google Compute Engine integration test machines cannot
	// resolve IPv6 addresses.
	// As of April 2014, https://developers.google.com/compute/docs/networking
	// said that IPv6 is not yet supported.
	ln, ep, err := server.Listen(ctx, "tcp4", "127.0.0.1:0", principal.BlessingStore().Default())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go acceptLoop(&wg, ln)

	nep := &inaming.Endpoint{
		Protocol: ep.Addr().Network(),
		Address:  ep.Addr().String(),
		RID:      ep.RoutingID(),
	}

	// Dial multiple VCs
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			cctx, _ := v23.WithPrincipal(ctx, testutil.NewPrincipal("client"))
			_, err := client.Dial(cctx, nep)
			errCh <- err
		}()
	}
	for i := 0; i < 10; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	// They should all be on the same VIF.
	if n := numVIFs(client); n != 1 {
		t.Errorf("Client has %d VIFs, want 1\n%v", n, debugString(client))
	}
	ln.Close()
	wg.Wait()
}

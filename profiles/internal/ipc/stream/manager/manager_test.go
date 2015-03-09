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
	"strings"
	"testing"
	"time"

	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
	"v.io/x/ref/profiles/internal/ipc/stream"

	"v.io/x/ref/lib/expect"
	"v.io/x/ref/lib/modules"
	"v.io/x/ref/lib/testutil"
	tsecurity "v.io/x/ref/lib/testutil/security"
	_ "v.io/x/ref/profiles/internal/ipc/protocols/tcp"
	_ "v.io/x/ref/profiles/internal/ipc/protocols/ws"
	"v.io/x/ref/profiles/internal/ipc/stream/vc"
	"v.io/x/ref/profiles/internal/ipc/version"
	inaming "v.io/x/ref/profiles/internal/naming"
)

func newPrincipal(defaultBlessing string) vc.LocalPrincipal {
	return vc.LocalPrincipal{tsecurity.NewPrincipal(defaultBlessing)}
}

func init() {
	modules.RegisterChild("runServer", "", runServer)
}

// We write our own TestMain here instead of relying on v23 test generate because
// we need to set runtime.GOMAXPROCS.
func TestMain(m *testing.M) {
	testutil.Init()
	// testutil.Init sets GOMAXPROCS to NumCPU.  We want to force
	// GOMAXPROCS to remain at 1, in order to trigger a particular race
	// condition that occurs when closing the server; also, using 1 cpu
	// introduces less variance in the behavior of the test.
	runtime.GOMAXPROCS(1)
	if modules.IsModulesProcess() {
		if err := modules.Dispatch(); err != nil {
			fmt.Fprintf(os.Stderr, "modules.Dispatch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

func testSimpleFlow(t *testing.T, protocol string) {
	server := InternalNew(naming.FixedRoutingID(0x55555555))
	client := InternalNew(naming.FixedRoutingID(0xcccccccc))

	ln, ep, err := server.Listen(protocol, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	data := "the dark knight rises"
	var clientVC stream.VC
	var clientF1 stream.Flow
	go func() {
		if clientVC, err = client.Dial(ep); err != nil {
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
	// Opening a new VC should fail fast.
	if _, err := client.Dial(ep); err == nil {
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
	client := InternalNew(naming.FixedRoutingID(0xcccccccc))

	ch := make(chan error)
	go func() {
		// 203.0.113.0 is TEST-NET-3 from RFC5737
		ep, _ := inaming.NewEndpoint(naming.FormatEndpoint("tcp", "203.0.113.10:80"))
		_, err := client.Dial(ep, &DialTimeout{time.Second})
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
	var (
		server = InternalNew(naming.FixedRoutingID(0x55555555))
		client = InternalNew(naming.FixedRoutingID(0xcccccccc))

		clientPrincipal = newPrincipal("client")
		serverPrincipal = newPrincipal("server")
		clientKey       = clientPrincipal.Principal.PublicKey()
		serverBlessings = serverPrincipal.Principal.BlessingStore().Default()
	)
	// VCSecurityLevel is intentionally not provided to Listen - to test
	// default behavior.
	ln, ep, err := server.Listen(protocol, "127.0.0.1:0", serverPrincipal)
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
		// VCSecurityLevel is intentionally not provided to Dial - to
		// test default behavior.
		vc, err := client.Dial(ep, clientPrincipal)
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
	server := InternalNew(naming.FixedRoutingID(0xcafe))
	ln1, ep1, err1 := server.Listen("tcp", "127.0.0.1:0")
	ln2, ep2, err2 := server.Listen("tcp", "127.0.0.1:0")
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

func acceptLoop(ln stream.Listener) {
	for {
		f, err := ln.Accept()
		if err != nil {
			return
		}
		f.Close()
	}
}

func TestCloseListener(t *testing.T) {
	testCloseListener(t, "tcp")
}

func TestCloseListenerWS(t *testing.T) {
	testCloseListener(t, "ws")
}

func testCloseListener(t *testing.T, protocol string) {
	server := InternalNew(naming.FixedRoutingID(0x5e97e9))

	ln, ep, err := server.Listen(protocol, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// Server will just listen for flows and close them.
	go acceptLoop(ln)
	client := InternalNew(naming.FixedRoutingID(0xc1e41))
	if _, err = client.Dial(ep); err != nil {
		t.Fatal(err)
	}
	ln.Close()
	client = InternalNew(naming.FixedRoutingID(0xc1e42))
	if _, err := client.Dial(ep); err == nil {
		t.Errorf("client.Dial(%q) should have failed", ep)
	}
}

func TestShutdown(t *testing.T) {
	server := InternalNew(naming.FixedRoutingID(0x5e97e9))
	ln, _, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// Server will just listen for flows and close them.
	go acceptLoop(ln)
	if n, expect := numListeners(server), 1; n != expect {
		t.Errorf("expecting %d listeners, got %d for %s", n, expect, debugString(server))
	}
	server.Shutdown()
	if _, _, err := server.Listen("tcp", "127.0.0.1:0"); err == nil {
		t.Error("server should have shut down")
	}
	if n, expect := numListeners(server), 0; n != expect {
		t.Errorf("expecting %d listeners, got %d for %s", n, expect, debugString(server))
	}
}

func TestShutdownEndpoint(t *testing.T) {
	testShutdownEndpoint(t, "tcp")
}

func TestShutdownEndpointWS(t *testing.T) {
	testShutdownEndpoint(t, "ws")
}

func testShutdownEndpoint(t *testing.T, protocol string) {
	server := InternalNew(naming.FixedRoutingID(0x55555555))
	client := InternalNew(naming.FixedRoutingID(0xcccccccc))

	ln, ep, err := server.Listen(protocol, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	// Server will just listen for flows and close them.
	go acceptLoop(ln)

	vc, err := client.Dial(ep)
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
}

/* TLS + resumption + channel bindings is broken: <https://secure-resumption.com/#channelbindings>.
func TestSessionTicketCache(t *testing.T) {
	server := InternalNew(naming.FixedRoutingID(0x55555555))
	_, ep, err := server.Listen("tcp", "127.0.0.1:0", newPrincipal("server"))
	if err != nil {
		t.Fatal(err)
	}

	client := InternalNew(naming.FixedRoutingID(0xcccccccc))
	if _, err = client.Dial(ep, newPrincipal("TestSessionTicketCacheClient")); err != nil {
		t.Fatalf("Dial(%q) failed: %v", ep, err)
	}

	if _, ok := client.(*manager).sessionCache.Get(ep.String()); !ok {
		t.Fatalf("SessionTicket from TLS handshake not cached")
	}
}
*/

func testMultipleVCs(t *testing.T, protocol string) {
	server := InternalNew(naming.FixedRoutingID(0x55555555))
	client := InternalNew(naming.FixedRoutingID(0xcccccccc))

	const nVCs = 2
	const data = "bugs bunny"

	// Have the server read from each flow and write to rchan.
	rchan := make(chan string)
	ln, ep, err := server.Listen(protocol, "127.0.0.1:0")
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
		vcs[i], err = client.Dial(ep)
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
	server := InternalNew(naming.FixedRoutingID(0x55555555))
	client := InternalNew(naming.FixedRoutingID(0xcccccccc))

	// Using "tcp4" instead of "tcp" because the latter can end up with IPv6
	// addresses and our Google Compute Engine integration test machines cannot
	// resolve IPv6 addresses.
	// As of April 2014, https://developers.google.com/compute/docs/networking
	// said that IPv6 is not yet supported.
	ln, ep, err := server.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go acceptLoop(ln)

	// We'd like an endpoint that contains an address that's different than the
	// one used for the connection. In practice this is awkward to achieve since
	// we don't want to listen on ":0" since that will annoy firewalls. Instead we
	// listen on 127.0.0.1 and we fabricate an endpoint that doesn't contain
	// 127.0.0.1 by using ":0" to create it. This leads to an endpoint such that
	// the address encoded in the endpoint (e.g. "0.0.0.0:55324") is different
	// from the address of the connection (e.g. "127.0.0.1:55324").
	_, port, _ := net.SplitHostPort(ep.Addr().String())
	nep := version.Endpoint(ep.Addr().Network(), net.JoinHostPort("", port), ep.RoutingID())

	// Dial multiple VCs
	for i := 0; i < 2; i++ {
		if _, err = client.Dial(nep); err != nil {
			t.Fatalf("Dial #%d failed: %v", i, err)
		}
	}
	// They should all be on the same VIF.
	if n := numVIFs(client); n != 1 {
		t.Errorf("Client has %d VIFs, want 1\n%v", n, debugString(client))
	}
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
	client := InternalNew(naming.FixedRoutingID(0xcccccccc))
	sh, err := modules.NewShell(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(nil, nil)
	h, err := sh.Start("runServer", nil, protocol, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	addr := s.ReadLine()

	ep, err := inaming.NewEndpoint(naming.FormatEndpoint(protocol, addr))
	if err != nil {
		t.Fatalf("inaming.NewEndpoint(%q): %v", addr, err)
	}
	if _, err := client.Dial(ep); err != nil {
		t.Fatal(err)
	}
	h.Shutdown(nil, os.Stderr)

	// A new VC cannot be created since the server is dead
	if _, err := client.Dial(ep); err == nil {
		t.Fatal("Expected client.Dial to fail since server is dead")
	}

	h, err = sh.Start("runServer", nil, protocol, addr)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s = expect.NewSession(t, h.Stdout(), time.Minute)
	// Restarting the server, listening on the same address as before
	if addr2 := s.ReadLine(); addr2 != addr || err != nil {
		t.Fatalf("Got (%q, %v) want (%q, nil)", addr2, err, addr)
	}
	if _, err := client.Dial(ep); err != nil {
		t.Fatal(err)
	}
}

func runServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	server := InternalNew(naming.FixedRoutingID(0x55555555))
	_, ep, err := server.Listen(args[0], args[1])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return err
	}
	fmt.Fprintln(stdout, ep.Addr())
	// Live forever (till the process is explicitly killed)
	modules.WaitForEOF(stdin)
	return nil
}

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
	vlog.VI(1).Infof("write sending %d bytes", len(data))
	if n, err := f.Write([]byte(data)); err != nil {
		return fmt.Errorf("Write returned (%d, %v)", n, err)
	}
	return nil
}

func TestRegistration(t *testing.T) {
	server := InternalNew(naming.FixedRoutingID(0x55555555))
	client := InternalNew(naming.FixedRoutingID(0xcccccccc))

	dialer := func(_, _ string, _ time.Duration) (net.Conn, error) {
		return nil, fmt.Errorf("tn.Dial")
	}
	listener := func(_, _ string) (net.Listener, error) {
		return nil, fmt.Errorf("tn.Listen")
	}
	ipc.RegisterProtocol("tn", dialer, listener)

	_, _, err := server.Listen("tnx", "127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "unknown network tnx") {
		t.Fatal("expected error is missing (%v)", err)
	}

	_, _, err = server.Listen("tn", "127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "tn.Listen") {
		t.Fatal("expected error is missing (%v)", err)
	}

	// Need a functional listener to test Dial.
	listener = func(_, addr string) (net.Listener, error) {
		return net.Listen("tcp", addr)
	}

	if got, want := ipc.RegisterProtocol("tn", dialer, listener), true; got != want {
		t.Errorf("got %t, want %t", got, want)
	}

	_, ep, err := server.Listen("tn", "127.0.0.1:0")
	if err != nil {
		t.Errorf("unexpected error %s", err)
	}

	_, err = client.Dial(ep)
	if err == nil || !strings.Contains(err.Error(), "tn.Dial") {
		t.Fatal("expected error is missing (%v)", err)
	}
}

func TestBlessingNamesInEndpoint(t *testing.T) {
	var (
		p     = newPrincipal("default")
		b1, _ = p.Principal.BlessSelf("dev.v.io/users/foo@bar.com/devices/desktop/app/myapp")
		b2, _ = p.Principal.BlessSelf("otherblessing")
		b, _  = security.UnionOfBlessings(b1, b2)
		bopt  = options.ServerBlessings{b}

		server = InternalNew(naming.FixedRoutingID(0x1))

		tests = []struct {
			opts      []stream.ListenerOpt
			blessings []string
			err       bool
		}{
			{
				// Use the default blessings when only a principal is provided
				opts:      []stream.ListenerOpt{p},
				blessings: []string{"default"},
			},
			{
				// Respect options.ServerBlessings if provided
				opts:      []stream.ListenerOpt{p, bopt},
				blessings: []string{"dev.v.io/users/foo@bar.com/devices/desktop/app/myapp", "otherblessing"},
			},
			{
				// It is an error to provide options.ServerBlessings without vc.LocalPrincipal
				opts: []stream.ListenerOpt{bopt},
				err:  true,
			},
			{
				// It is an error to provide inconsistent options.ServerBlessings and vc.LocalPrincipal
				opts: []stream.ListenerOpt{newPrincipal("random"), bopt},
				err:  true,
			},
		}
	)
	// p must recognize its own blessings!
	p.AddToRoots(bopt.Blessings)
	for idx, test := range tests {
		ln, ep, err := server.Listen("tcp", "127.0.0.1:0", test.opts...)
		if (err != nil) != test.err {
			t.Errorf("test #%d: Got error %v, wanted error: %v", idx, err, test.err)
		}
		if err != nil {
			continue
		}
		ln.Close()
		got, want := ep.BlessingNames(), test.blessings
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("test #%d: Got %v, want %v", idx, got, want)
		}
	}
}

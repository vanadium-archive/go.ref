// Use a different package for the tests to ensure that only the exported API is used.

package vc_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/id"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/sectest"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	"veyron.io/veyron/veyron/runtimes/google/lib/bqueue"
	"veyron.io/veyron/veyron/runtimes/google/lib/bqueue/drrqueue"
	"veyron.io/veyron/veyron/runtimes/google/lib/iobuf"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/ipc/version"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
)

const (
	// Convenience alias to avoid conflicts between the package name "vc" and variables called "vc".
	DefaultBytesBufferedPerFlow = vc.DefaultBytesBufferedPerFlow
	// Shorthands
	SecurityNone = veyron2.VCSecurityNone
	SecurityTLS  = veyron2.VCSecurityConfidential

	LatestVersion = version.IPCVersion3
)

// testFlowEcho writes a random string of 'size' bytes on the flow and then
// ensures that the same string is read back.
func testFlowEcho(t *testing.T, flow stream.Flow, size int) {
	defer flow.Close()
	wrote := testutil.RandomBytes(size)
	go func() {
		buf := wrote
		for len(buf) > 0 {
			limit := 1 + testutil.Rand.Intn(len(buf)) // Random number in [1, n]
			n, err := flow.Write(buf[:limit])
			if n != limit || err != nil {
				t.Errorf("Write returned (%d, %v) want (%d, nil)", n, err, limit)
			}
			buf = buf[limit:]
		}
	}()

	total := 0
	read := make([]byte, size)
	buf := read
	for total < size {
		n, err := flow.Read(buf)
		if err != nil {
			t.Error(err)
			return
		}
		total += n
		buf = buf[n:]
	}
	if bytes.Compare(read, wrote) != 0 {
		t.Errorf("Data read != data written")
	}
}

func matchID(got, want security.PublicID) error {
	if want == nil {
		if got.Names() != nil {
			return fmt.Errorf("got identity with names: %v, want one with names: nil", got.Names())
		}
		return nil
	}
	if g, w := got.Names(), want.Names(); !reflect.DeepEqual(got.Names(), want.Names()) {
		return fmt.Errorf("got identity with names: %v, want one with names: %v", g, w)
	}
	if g, w := got.PublicKey(), want.PublicKey(); !reflect.DeepEqual(got.PublicKey(), want.PublicKey()) {
		return fmt.Errorf("got identity with public key: %v, want one with public key: %v", g, w)
	}
	return nil
}

func TestHandshake(t *testing.T) {
	// When SecurityNone is used, the blessings should not be sent over the wire.
	var (
		client    = sectest.NewPrincipal("client")
		server    = sectest.NewPrincipal("server")
		h, vc     = New(SecurityNone, LatestVersion, client, server)
		flow, err = vc.Connect()
	)
	defer h.Close()
	if err != nil {
		t.Fatal(err)
	}
	// TODO(ashankar): This should not be nil, but rather a dummy object (whose public
	// key does not match that of the principal)
	if flow.RemoteBlessings() != nil {
		t.Errorf("Server sent blessing %v over insecure transport", flow.RemoteBlessings())
	}
	if flow.LocalBlessings() != nil {
		t.Errorf("Client sent blessing %v over insecure transport", flow.LocalBlessings())
	}
}

func TestHandshakeTLS(t *testing.T) {
	var (
		client  = sectest.NewPrincipal("client")
		server1 = sectest.NewPrincipal("server1")
		server2 = sectest.NewPrincipal("server2")

		testBlessings = func(server security.Principal, wantClient security.Blessings) error {
			h, vc := New(SecurityTLS, LatestVersion, client, server)
			defer h.Close()
			flow, err := vc.Connect()
			if err != nil {
				return fmt.Errorf("unable to create flow: %v", err)
			}
			if got, want := flow.RemoteBlessings(), server.BlessingStore().Default(); !reflect.DeepEqual(got, want) {
				return fmt.Errorf("Got blessings %v from server, want %v", got, want)
			}
			if got, want := flow.LocalBlessings(), wantClient; !reflect.DeepEqual(got, want) {
				return fmt.Errorf("Client shared %v, wanted %v", got, want)
			}
			return nil
		}
	)
	// Setup client so that is has a specific blessing for S2.
	forServer1 := client.BlessingStore().Default()
	forServer2, err := client.BlessSelf("forS2")
	if err != nil {
		t.Fatal(err)
	}
	client.BlessingStore().Set(nil, security.AllPrincipals)
	client.BlessingStore().Set(forServer1, security.BlessingPattern("server1"))
	client.BlessingStore().Set(forServer2, security.BlessingPattern("server2"))

	// Make the clients and servers recognize each other as valid root certificate providers.
	for _, p := range []security.Principal{client, server1, server2} {
		for _, b := range []security.Blessings{server1.BlessingStore().Default(), server2.BlessingStore().Default(), client.BlessingStore().Default()} {
			if err := p.AddToRoots(b); err != nil {
				t.Fatalf("%v.AddToRoots(%v): %v", p, b, err)
			}
		}
	}

	if err := testBlessings(server1, forServer1); err != nil {
		t.Error(err)
	}
	if err := testBlessings(server2, forServer2); err != nil {
		t.Error(err)
	}
}

func testConnect_Small(t *testing.T, security veyron2.VCSecurityLevel) {
	h, vc := New(security, LatestVersion, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"))
	defer h.Close()
	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	testFlowEcho(t, flow, 10)
}
func TestConnect_Small(t *testing.T)    { testConnect_Small(t, SecurityNone) }
func TestConnect_SmallTLS(t *testing.T) { testConnect_Small(t, SecurityTLS) }

func testConnect(t *testing.T, security veyron2.VCSecurityLevel) {
	h, vc := New(security, LatestVersion, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"))
	defer h.Close()
	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	testFlowEcho(t, flow, 10*DefaultBytesBufferedPerFlow)
}
func TestConnect(t *testing.T)    { testConnect(t, SecurityNone) }
func TestConnectTLS(t *testing.T) { testConnect(t, SecurityTLS) }

func testConnect_Version2(t *testing.T, security veyron2.VCSecurityLevel) {
	h, vc := New(security, version.IPCVersion2, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"))
	defer h.Close()
	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	testFlowEcho(t, flow, 10)
}
func TestConnect_Version2(t *testing.T)    { testConnect_Version2(t, SecurityNone) }
func TestConnect_Version2TLS(t *testing.T) { testConnect_Version2(t, SecurityTLS) }

// helper function for testing concurrent operations on multiple flows over the
// same VC.  Such tests are most useful when running the race detector.
// (go test -race ...)
func testConcurrentFlows(t *testing.T, security veyron2.VCSecurityLevel, flows, gomaxprocs int) {
	mp := runtime.GOMAXPROCS(gomaxprocs)
	defer runtime.GOMAXPROCS(mp)
	h, vc := New(security, LatestVersion, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"))
	defer h.Close()

	var wg sync.WaitGroup
	wg.Add(flows)
	for i := 0; i < flows; i++ {
		go func(n int) {
			defer wg.Done()
			flow, err := vc.Connect()
			if err != nil {
				t.Error(err)
			} else {
				testFlowEcho(t, flow, (n+1)*DefaultBytesBufferedPerFlow)
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrentFlows_1(t *testing.T)    { testConcurrentFlows(t, SecurityNone, 10, 1) }
func TestConcurrentFlows_1TLS(t *testing.T) { testConcurrentFlows(t, SecurityTLS, 10, 1) }

func TestConcurrentFlows_10(t *testing.T)    { testConcurrentFlows(t, SecurityNone, 10, 10) }
func TestConcurrentFlows_10TLS(t *testing.T) { testConcurrentFlows(t, SecurityTLS, 10, 10) }

func testListen(t *testing.T, security veyron2.VCSecurityLevel) {
	data := "the dark knight"
	h, vc := New(security, LatestVersion, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"))
	defer h.Close()
	if err := h.VC.AcceptFlow(id.Flow(21)); err == nil {
		t.Errorf("Expected AcceptFlow on a new flow to fail as Listen was not called")
	}

	ln, err := vc.Listen()
	if err != nil {
		t.Fatalf("vc.Listen failed: %v", err)
		return
	}
	_, err = vc.Listen()
	if err == nil {
		t.Fatalf("Second call to vc.Listen should have failed")
		return
	}

	if err := h.VC.AcceptFlow(id.Flow(23)); err != nil {
		t.Fatal(err)
	}
	cipherdata, err := h.otherEnd.VC.Encrypt(id.Flow(23), iobuf.NewSlice([]byte(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.VC.DispatchPayload(id.Flow(23), cipherdata); err != nil {
		t.Fatal(err)
	}
	flow, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Error(err)
	}
	flow.Close()
	var buf [4096]byte
	if n, err := flow.Read(buf[:]); n != len(data) || err != nil || string(buf[:n]) != data {
		t.Errorf("Got (%d, %v) = %q, want (%d, nil) = %q", n, err, string(buf[:n]), len(data), data)
	}
	if n, err := flow.Read(buf[:]); n != 0 || err != io.EOF {
		t.Errorf("Got (%d, %v) want (0, %v)", n, err, io.EOF)
	}
}
func TestListen(t *testing.T)    { testListen(t, SecurityNone) }
func TestListenTLS(t *testing.T) { testListen(t, SecurityTLS) }

func testNewFlowAfterClose(t *testing.T, security veyron2.VCSecurityLevel) {
	h, _ := New(security, LatestVersion, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"))
	defer h.Close()
	h.VC.Close("reason")
	if err := h.VC.AcceptFlow(id.Flow(10)); err == nil {
		t.Fatalf("New flows should not be accepted once the VC is closed")
	}
}
func TestNewFlowAfterClose(t *testing.T)    { testNewFlowAfterClose(t, SecurityNone) }
func TestNewFlowAfterCloseTLS(t *testing.T) { testNewFlowAfterClose(t, SecurityTLS) }

func testConnectAfterClose(t *testing.T, security veyron2.VCSecurityLevel) {
	h, vc := New(security, LatestVersion, sectest.NewPrincipal("client"), sectest.NewPrincipal("server"))
	defer h.Close()
	h.VC.Close("myerr")
	if f, err := vc.Connect(); f != nil || err == nil || !strings.Contains(err.Error(), "myerr") {
		t.Fatalf("Got (%v, %v), want (nil, %q)", f, err, "myerr")
	}
}
func TestConnectAfterClose(t *testing.T)    { testConnectAfterClose(t, SecurityNone) }
func TestConnectAfterCloseTLS(t *testing.T) { testConnectAfterClose(t, SecurityTLS) }

// helper implements vc.Helper and also sets up a single VC.
type helper struct {
	VC *vc.VC
	bq bqueue.T

	mu       sync.Mutex
	otherEnd *helper // GUARDED_BY(mu)
}

// New creates both ends of a VC but returns only the "client" end (i.e., the
// one that initiated the VC). The "server" end (the one that "accepted" the VC)
// listens for flows and simply echoes data read.
func New(security veyron2.VCSecurityLevel, v version.IPCVersion, client, server security.Principal) (*helper, stream.VC) {
	clientH := &helper{bq: drrqueue.New(vc.MaxPayloadSizeBytes)}
	serverH := &helper{bq: drrqueue.New(vc.MaxPayloadSizeBytes)}
	clientH.otherEnd = serverH
	serverH.otherEnd = clientH

	clientEP := endpoint(naming.FixedRoutingID(0xcccccccccccccccc))
	serverEP := endpoint(naming.FixedRoutingID(0x5555555555555555))

	vci := id.VC(1234)

	clientParams := vc.Params{
		VCI:      vci,
		Dialed:   true,
		LocalEP:  clientEP,
		RemoteEP: serverEP,
		Pool:     iobuf.NewPool(0),
		Helper:   clientH,
		Version:  v,
	}
	serverParams := vc.Params{
		VCI:      vci,
		LocalEP:  serverEP,
		RemoteEP: clientEP,
		Pool:     iobuf.NewPool(0),
		Helper:   serverH,
		Version:  v,
	}

	clientH.VC = vc.InternalNew(clientParams)
	serverH.VC = vc.InternalNew(serverParams)
	clientH.AddReceiveBuffers(vci, vc.SharedFlowID, vc.DefaultBytesBufferedPerFlow)

	go clientH.pipeLoop(serverH.VC)
	go serverH.pipeLoop(clientH.VC)

	c := serverH.VC.HandshakeAcceptedVC(security, vc.LocalPrincipal{server})
	if err := clientH.VC.HandshakeDialedVC(security, vc.LocalPrincipal{client}); err != nil {
		panic(err)
	}
	hr := <-c
	if hr.Error != nil {
		panic(hr.Error)
	}
	go acceptLoop(hr.Listener)
	return clientH, clientH.VC
}

// pipeLoop forwards slices written to h.bq to dst.
func (h *helper) pipeLoop(dst *vc.VC) {
	for {
		w, bufs, err := h.bq.Get(nil)
		if err != nil {
			return
		}
		fid := id.Flow(w.ID())
		for _, b := range bufs {
			cipher, err := h.VC.Encrypt(fid, b)
			if err != nil {
				panic(err)
			}
			if err := dst.DispatchPayload(fid, cipher); err != nil {
				panic(err)
				return
			}
		}
		if w.IsDrained() {
			h.VC.ShutdownFlow(fid)
			dst.ShutdownFlow(fid)
		}
	}
}

func acceptLoop(ln stream.Listener) {
	for {
		f, err := ln.Accept()
		if err != nil {
			return
		}
		go echoLoop(f)
	}
}

func echoLoop(flow stream.Flow) {
	var buf [vc.DefaultBytesBufferedPerFlow * 20]byte
	for {
		n, err := flow.Read(buf[:])
		if err == io.EOF {
			return
		}
		if err == nil {
			_, err = flow.Write(buf[:n])
		}
		if err != nil {
			panic(err)
		}
	}
}

func (h *helper) NotifyOfNewFlow(vci id.VC, fid id.Flow, bytes uint) {
	h.mu.Lock()
	if h.otherEnd != nil {
		if err := h.otherEnd.VC.AcceptFlow(fid); err != nil {
			panic(err)
		}
		h.otherEnd.VC.ReleaseCounters(fid, uint32(bytes))
	}
	h.mu.Unlock()
}

func (h *helper) AddReceiveBuffers(vci id.VC, fid id.Flow, bytes uint) {
	h.mu.Lock()
	if h.otherEnd != nil {
		h.otherEnd.VC.ReleaseCounters(fid, uint32(bytes))
	}
	h.mu.Unlock()
}

func (h *helper) NewWriter(vci id.VC, fid id.Flow) (bqueue.Writer, error) {
	return h.bq.NewWriter(bqueue.ID(fid), 0, DefaultBytesBufferedPerFlow)
}

func (h *helper) Close() {
	h.VC.Close("helper closed")
	h.bq.Close()
	h.mu.Lock()
	otherEnd := h.otherEnd
	h.otherEnd = nil
	h.mu.Unlock()
	if otherEnd != nil {
		otherEnd.mu.Lock()
		otherEnd.otherEnd = nil
		otherEnd.mu.Unlock()
		otherEnd.Close()
	}
}

type endpoint naming.RoutingID

func (e endpoint) Network() string             { return "test" }
func (e endpoint) String() string              { return naming.RoutingID(e).String() }
func (e endpoint) RoutingID() naming.RoutingID { return naming.RoutingID(e) }
func (e endpoint) Addr() net.Addr              { return nil }

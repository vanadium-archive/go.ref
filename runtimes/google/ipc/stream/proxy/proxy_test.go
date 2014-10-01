package proxy_test

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"

	_ "veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/proxy"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	isecurity "veyron.io/veyron/veyron/runtimes/google/security"
)

func newID(name string) security.PrivateID {
	id, err := isecurity.NewPrivateID(name, nil)
	if err != nil {
		panic(err)
	}
	return id
}

func TestProxy(t *testing.T) {
	proxy, err := proxy.New(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), nil, "tcp", "127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Shutdown()

	// Create the stream.Manager for the server.
	server1 := manager.InternalNew(naming.FixedRoutingID(0x1111111111111111))
	defer server1.Shutdown()
	// Setup a stream.Listener that will accept VCs and Flows routed
	// through the proxy.
	ln1, ep1, err := server1.Listen(proxy.Endpoint().Network(), proxy.Endpoint().String())
	if err != nil {
		t.Fatal(err)
	}
	defer ln1.Close()

	// Create the stream.Manager for a second server.
	server2 := manager.InternalNew(naming.FixedRoutingID(0x2222222222222222))
	defer server2.Shutdown()
	// Setup a stream.Listener that will accept VCs and Flows routed
	// through the proxy.
	ln2, ep2, err := server2.Listen(proxy.Endpoint().Network(), proxy.Endpoint().String())
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()

	// Create the stream.Manager for a client.
	client := manager.InternalNew(naming.FixedRoutingID(0xcccccccccccccccc))
	defer client.Shutdown()

	cases := []struct {
		client stream.Manager
		ln     stream.Listener
		ep     naming.Endpoint
	}{
		{client, ln1, ep1},  // client writing to server1
		{server1, ln2, ep2}, // server1 writing to server2
		{server1, ln1, ep1}, // server1 writing to itself
	}

	const written = "the dough rises"
	for i, c := range cases {
		name := fmt.Sprintf("case #%d(write to %v):", i, c.ep)
		// Accept a single flow and write out what is read to readChan
		readChan := make(chan string)
		go readFlow(t, c.ln, readChan)
		if err := writeFlow(c.client, c.ep, written); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		// Validate that the data read is the same as the data written.
		if read := <-readChan; read != written {
			t.Errorf("case #%d: Read %q, wrote %q", i, read, written)
		}
	}
}

func TestDuplicateRoutingID(t *testing.T) {
	proxy, err := proxy.New(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), nil, "tcp", "127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Shutdown()

	// Create the stream.Manager for server1 and server2, both with the same routing ID
	serverRID := naming.FixedRoutingID(0x5555555555555555)
	server1 := manager.InternalNew(serverRID)
	server2 := manager.InternalNew(serverRID)
	defer server1.Shutdown()
	defer server2.Shutdown()

	// First server to claim serverRID should win.
	ln1, ep1, err := server1.Listen(proxy.Endpoint().Network(), proxy.Endpoint().String())
	if err != nil {
		t.Fatal(err)
	}
	defer ln1.Close()

	ln2, ep2, err := server2.Listen(proxy.Endpoint().Network(), proxy.Endpoint().String())
	if pattern := "routing id 00000000000000005555555555555555 is already being proxied"; err == nil || !strings.Contains(err.Error(), pattern) {
		t.Errorf("Got (%v, %v, %v) want error \"...%v\" (ep1:%v)", ln2, ep2, err, pattern, ep1)
	}
}

func TestProxyIdentity(t *testing.T) {
	proxyID := newID("proxy")
	proxy, err := proxy.New(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), proxyID, "tcp", "127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Shutdown()

	other := manager.InternalNew(naming.FixedRoutingID(0xcccccccccccccccc))
	defer other.Shutdown()

	vc, err := other.Dial(proxy.Endpoint())
	if err != nil {
		t.Fatal(err)
	}

	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprintf("%v", flow.RemoteID()), fmt.Sprintf("%v", proxyID.PublicID()); got != want {
		t.Errorf("Proxy has identity %q want %q", flow.RemoteID(), want)
	}
}

func TestServerIdentity(t *testing.T) {
	proxy, err := proxy.New(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), nil, "tcp", "127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}

	server := manager.InternalNew(naming.FixedRoutingID(0x5555555555555555))
	defer server.Shutdown()
	serverID := newID("server")
	if err != nil {
		t.Fatal(err)
	}
	ln, ep, err := server.Listen(proxy.Endpoint().Network(), proxy.Endpoint().String(), vc.FixedLocalID(serverID))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			if _, err := ln.Accept(); err != nil {
				return
			}
		}
	}()

	client := manager.InternalNew(naming.FixedRoutingID(0xcccccccccccccccc))
	defer client.Shutdown()
	vc, err := client.Dial(ep)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := vc.Connect()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := flow.RemoteID(), serverID.PublicID(); !reflect.DeepEqual(got, want) {
		t.Errorf("Got %q want %q", got, want)
	}
}

func TestHostPort(t *testing.T) {
	proxy, err := proxy.New(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), nil, "tcp", "127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Shutdown()
	server := manager.InternalNew(naming.FixedRoutingID(0x5555555555555555))
	defer server.Shutdown()
	addr := proxy.Endpoint().Addr().String()
	port := addr[strings.LastIndex(addr, ":"):]
	ln, _, err := server.Listen("veyron", "127.0.0.1"+port)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

func TestClientBecomesServer(t *testing.T) {
	proxy, err := proxy.New(naming.FixedRoutingID(0xbbbbbbbbbbbbbbbb), nil, "tcp", "127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}
	server := manager.InternalNew(naming.FixedRoutingID(0x5555555555555555))
	client1 := manager.InternalNew(naming.FixedRoutingID(0x1111111111111111))
	client2 := manager.InternalNew(naming.FixedRoutingID(0x2222222222222222))
	defer proxy.Shutdown()
	defer server.Shutdown()
	defer client1.Shutdown()
	defer client2.Shutdown()

	lnS, epS, err := server.Listen(proxy.Endpoint().Network(), proxy.Endpoint().String())
	if err != nil {
		t.Fatal(err)
	}
	defer lnS.Close()
	rchan := make(chan string)

	// client1 must connect to the proxy to speak to the server.
	// Keep a VC and Flow open to the server, to ensure that the proxy
	// maintains routing information (at some point, inactive VIFs
	// should be garbage collected, so this ensures that the VIF
	// is "active")
	if vc, err := client1.Dial(epS); err != nil {
		t.Fatal(err)
	} else if flow, err := vc.Connect(); err != nil {
		t.Fatal(err)
	} else {
		defer flow.Close()
	}

	// Now client1 becomes a server
	lnC, epC, err := client1.Listen(proxy.Endpoint().Network(), proxy.Endpoint().String())
	if err != nil {
		t.Fatal(err)
	}
	defer lnC.Close()
	// client2 should be able to talk to client1 through the proxy
	rchan = make(chan string)
	go readFlow(t, lnC, rchan)
	if err := writeFlow(client2, epC, "daffy duck"); err != nil {
		t.Fatal("client2 failed to chat with client1: %v", err)
	}
	if got, want := <-rchan, "daffy duck"; got != want {
		t.Fatal("client2->client1 got %q want %q", got, want)
	}
}

func writeFlow(mgr stream.Manager, ep naming.Endpoint, data string) error {
	vc, err := mgr.Dial(ep)
	if err != nil {
		return fmt.Errorf("manager.Dial(%v) failed: %v", ep, err)
	}
	flow, err := vc.Connect()
	if err != nil {
		return fmt.Errorf("vc.Connect failed: %v", err)
	}
	defer flow.Close()
	if _, err := flow.Write([]byte(data)); err != nil {
		return fmt.Errorf("flow.Write failed: %v", err)
	}
	return nil
}

func readFlow(t *testing.T, ln stream.Listener, read chan<- string) {
	defer close(read)
	flow, err := ln.Accept()
	if err != nil {
		t.Error(err)
		return
	}
	var tmp [1024]byte
	var buf bytes.Buffer
	for {
		n, err := flow.Read(tmp[:])
		if err == io.EOF {
			read <- buf.String()
			return
		}
		if err != nil {
			t.Error(err)
			return
		}
		buf.Write(tmp[:n])
	}
}

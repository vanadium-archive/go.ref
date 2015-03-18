package vif_test

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"testing"
	"time"

	"v.io/v23/naming"
	"v.io/v23/rpc"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/profiles/internal/rpc/stream/vif"
	tsecurity "v.io/x/ref/test/security"
)

var supportsIPv6 bool

func init() {
	rpc.RegisterProtocol("unix", net.DialTimeout, net.Listen)

	// Check whether the platform supports IPv6.
	ln, err := net.Listen("tcp6", "[::1]:0")
	defer ln.Close()
	if err == nil {
		supportsIPv6 = true
	}
}

func newConn(network, address string) (net.Conn, net.Conn, error) {
	dfunc, lfunc, _ := rpc.RegisteredProtocol(network)
	ln, err := lfunc(network, address)
	if err != nil {
		return nil, nil, err
	}
	defer ln.Close()

	done := make(chan net.Conn)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		conn.Read(make([]byte, 1)) // Read a dummy byte.
		done <- conn
	}()

	conn, err := dfunc(ln.Addr().Network(), ln.Addr().String(), 1*time.Second)
	if err != nil {
		return nil, nil, err
	}
	// Write a dummy byte since wsh listener waits for the magic bytes for ws.
	conn.Write([]byte("."))
	return conn, <-done, nil
}

func newVIF(c, s net.Conn) (*vif.VIF, *vif.VIF, error) {
	done := make(chan *vif.VIF)
	go func() {
		principal := tsecurity.NewPrincipal("accepted")
		blessings := principal.BlessingStore().Default()
		vf, err := vif.InternalNewAcceptedVIF(s, naming.FixedRoutingID(0x5), principal, blessings, nil)
		if err != nil {
			panic(err)
		}
		done <- vf
	}()

	vf, err := vif.InternalNewDialedVIF(c, naming.FixedRoutingID(0xc), tsecurity.NewPrincipal("dialed"), nil)
	if err != nil {
		return nil, nil, err
	}
	return vf, <-done, nil
}

func diff(a, b []string) []string {
	m := make(map[string]struct{})
	for _, x := range b {
		m[x] = struct{}{}
	}
	d := make([]string, 0, len(a))
	for _, x := range a {
		if _, ok := m[x]; !ok {
			d = append(d, x)
		}
	}
	return d
}

func TestSetBasic(t *testing.T) {
	sockdir, err := ioutil.TempDir("", "TestSetBasic")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockdir)

	all := rpc.RegisteredProtocols()
	unknown := naming.UnknownProtocol
	tests := []struct {
		network, address string
		compatibles      []string
	}{
		{"tcp", "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{"tcp4", "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{"tcp", "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{"tcp6", "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{"ws", "127.0.0.1:0", []string{"ws", "ws4", "wsh", "wsh4", unknown}},
		{"ws4", "127.0.0.1:0", []string{"ws", "ws4", "wsh", "wsh4", unknown}},
		{"ws", "[::1]:0", []string{"ws", "ws6", "wsh", "wsh6", unknown}},
		{"ws6", "[::1]:0", []string{"ws", "ws6", "wsh", "wsh6", unknown}},
		// wsh dial always uses tcp.
		{"wsh", "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{"wsh4", "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{"wsh", "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{"wsh6", "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{unknown, "127.0.0.1:0", []string{"tcp", "tcp4", "wsh", "wsh4", unknown}},
		{unknown, "[::1]:0", []string{"tcp", "tcp6", "wsh", "wsh6", unknown}},
		{"unix", path.Join(sockdir, "socket"), []string{"unix"}},
	}

	set := vif.NewSet()
	for _, test := range tests {
		if test.address == "[::1]:0" && !supportsIPv6 {
			continue
		}

		name := fmt.Sprintf("(%q, %q)", test.network, test.address)

		c, s, err := newConn(test.network, test.address)
		if err != nil {
			t.Fatal(err)
		}
		vf, _, err := newVIF(c, s)
		if err != nil {
			t.Fatal(err)
		}
		a := c.RemoteAddr()

		set.Insert(vf)
		for _, n := range test.compatibles {
			if found := set.Find(n, a.String()); found == nil {
				t.Fatalf("%s: Got nil, but want [%v] on Find(%q, %q))", name, vf, n, a)
			}
		}

		for _, n := range diff(all, test.compatibles) {
			if v := set.Find(n, a.String()); v != nil {
				t.Fatalf("%s: Got [%v], but want nil on Find(%q, %q))", name, v, n, a)
			}
		}

		set.Delete(vf)
		for _, n := range all {
			if v := set.Find(n, a.String()); v != nil {
				t.Fatalf("%s: Got [%v], but want nil on Find(%q, %q))", name, v, n, a)
			}
		}
	}
}

func TestSetWithPipes(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	a1 := c1.RemoteAddr()
	a2 := c2.RemoteAddr()
	if a1.Network() != a2.Network() || a1.String() != a2.String() {
		t.Fatalf("This test was intended for distinct connections that have duplicate RemoteAddrs. "+
			"That does not seem to be the case with (%q, %q) and (%q, %q)",
			a1.Network(), a1, a2.Network(), a2)
	}

	vf1, _, err := newVIF(c1, s1)
	if err != nil {
		t.Fatal(err)
	}
	vf2, _, err := newVIF(c2, s2)
	if err != nil {
		t.Fatal(err)
	}

	set := vif.NewSet()
	set.Insert(vf1)
	if v := set.Find(a1.Network(), a1.String()); v != nil {
		t.Fatalf("Got [%v], but want nil on Find(%q, %q))", v, a1.Network(), a1)
	}
	if l := set.List(); len(l) != 1 || l[0] != vf1 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set.Insert(vf2)
	if v := set.Find(a2.Network(), a2.String()); v != nil {
		t.Fatalf("Got [%v], but want nil on Find(%q, %q))", v, a2.Network(), a2)
	}
	if l := set.List(); len(l) != 2 || l[0] != vf1 || l[1] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set.Delete(vf1)
	if l := set.List(); len(l) != 1 || l[0] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
	set.Delete(vf2)
	if l := set.List(); len(l) != 0 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
}

func TestSetWithUnixSocket(t *testing.T) {
	dir, err := ioutil.TempDir("", "TestSetWithUnixSocket")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	c1, s1, err := newConn("unix", path.Join(dir, "socket1"))
	if err != nil {
		t.Fatal(err)
	}
	c2, s2, err := newConn("unix", path.Join(dir, "socket2"))
	if err != nil {
		t.Fatal(err)
	}

	// The client side address is always unix:@ regardless of socket name.
	a1 := s1.RemoteAddr()
	a2 := s2.RemoteAddr()
	if a1.Network() != a2.Network() || a1.String() != a2.String() {
		t.Fatalf("This test was intended for distinct connections that have duplicate RemoteAddrs. "+
			"That does not seem to be the case with (%q, %q) and (%q, %q)",
			a1.Network(), a1, a2.Network(), a2)
	}

	_, vf1, err := newVIF(c1, s1)
	if err != nil {
		t.Fatal(err)
	}
	_, vf2, err := newVIF(c2, s2)
	if err != nil {
		t.Fatal(err)
	}

	set := vif.NewSet()
	set.Insert(vf1)
	if v := set.Find(a1.Network(), a1.String()); v != nil {
		t.Fatalf("Got [%v], but want nil on Find(%q, %q))", v, a1.Network(), a1)
	}
	if l := set.List(); len(l) != 1 || l[0] != vf1 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set.Insert(vf2)
	if v := set.Find(a2.Network(), a2.String()); v != nil {
		t.Fatalf("Got [%v], but want nil on Find(%q, %q))", v, a2.Network(), a2)
	}
	if l := set.List(); len(l) != 2 || l[0] != vf1 || l[1] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set.Delete(vf1)
	if l := set.List(); len(l) != 1 || l[0] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
	set.Delete(vf2)
	if l := set.List(); len(l) != 0 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
}

func TestSetInsertDelete(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	vf1, _, err := newVIF(c1, s1)
	if err != nil {
		t.Fatal(err)
	}
	vf2, _, err := newVIF(c2, s2)
	if err != nil {
		t.Fatal(err)
	}

	set1 := vif.NewSet()
	set2 := vif.NewSet()

	set1.Insert(vf1)
	if l := set1.List(); len(l) != 1 || l[0] != vf1 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
	set1.Insert(vf2)
	if l := set1.List(); len(l) != 2 || l[0] != vf1 || l[1] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	set2.Insert(vf1)
	set2.Insert(vf2)

	set1.Delete(vf1)
	if l := set1.List(); len(l) != 1 || l[0] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
	if l := set2.List(); len(l) != 2 || l[0] != vf1 || l[1] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	vf1.Close()
	if l := set1.List(); len(l) != 1 || l[0] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
	if l := set2.List(); len(l) != 1 || l[0] != vf2 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}

	vf2.Close()
	if l := set1.List(); len(l) != 0 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
	if l := set2.List(); len(l) != 0 {
		t.Errorf("Unexpected list of VIFs: %v", l)
	}
}

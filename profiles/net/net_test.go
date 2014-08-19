package net

import (
	"fmt"
	"net"
	"reflect"
	"testing"
)

// TODO(cnicolaou): more unit tests for:
// Monitor/Publish network settings
// PublicIPState
// Empty
// Has
// First
// etc.

func exampleIPState() {
	ip4, ip6, err := ipState()
	if err != nil {
		panic("failed to find any network interfaces!")
	}
	for ifc, addrs := range ip4 {
		for _, addr := range addrs {
			fmt.Printf("ipv4: %s: %s", ifc, addr)
		}
	}
	for ifc, addrs := range ip6 {
		for _, addr := range addrs {
			fmt.Printf("ipv6: %s: %s", ifc, addr)
		}
	}
}

var (
	a  = net.ParseIP("1.2.3.4")
	b  = net.ParseIP("1.2.3.5")
	c  = net.ParseIP("1.2.3.6")
	d  = net.ParseIP("1.2.3.7")
	a6 = net.ParseIP("2001:4860:0:2001::68")
	b6 = net.ParseIP("2001:4860:0:2001::69")
	c6 = net.ParseIP("2001:4860:0:2001::70")
	d6 = net.ParseIP("2001:4860:0:2001::71")
)

func TestRemoved(t *testing.T) {
	aif := make(ipAndIf)
	bif := make(ipAndIf)
	aif["eth0"] = []net.IP{a, b, c, a6, b6, c6}
	aif["eth1"] = []net.IP{a, b, c, a6, b6, c6}

	// no changes.
	got, want := findRemoved(aif, aif), ipAndIf{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// missing interfaces
	got, want = findRemoved(aif, bif), ipAndIf{"eth0": nil, "eth1": nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// missing some addresses on a single interface
	bif["eth0"] = []net.IP{a, b, a6, b6}
	got, want = findRemoved(aif, bif), ipAndIf{
		"eth0": []net.IP{c, c6},
		"eth1": nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// change an address on a single interface
	bif["eth0"] = []net.IP{a, b, c, a6, b6, c6}
	bif["eth1"] = []net.IP{a, b, c, a6, b6, c6}
	bif["eth1"][2] = d
	got, want = findRemoved(aif, bif), ipAndIf{
		"eth1": []net.IP{c},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// additions
	bif["eth0"] = []net.IP{a, b, c, a6, b6, c6, d6}
	bif["eth1"] = []net.IP{a, b, c, a6, b6, c6, d6}
	got, want = findRemoved(aif, bif), ipAndIf{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestAdded(t *testing.T) {
	aif := make(ipAndIf)
	bif := make(ipAndIf)
	aif["eth0"] = []net.IP{a, b, c, a6, b6, c6}

	// no changes.
	got, want := findAdded(aif, aif), ipAndIf{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// added interface
	bif["eth0"] = []net.IP{a, b, c, a6, b6, c6}
	bif["eth1"] = []net.IP{a, b, c, a6, b6, c6}
	got, want = findAdded(aif, bif), ipAndIf{
		"eth1": []net.IP{a, b, c, a6, b6, c6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// added some address on a single interface
	bif["eth0"] = []net.IP{a, b, c, d, a6, b6, c6, d6}
	delete(bif, "eth1")
	got, want = findAdded(aif, bif), ipAndIf{
		"eth0": []net.IP{d, d6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// removals
	bif["eth0"] = []net.IP{a, b, c, b6}
	got, want = findAdded(aif, bif), ipAndIf{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

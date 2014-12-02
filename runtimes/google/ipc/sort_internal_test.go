package ipc

import (
	"reflect"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2/ipc/version"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/vlog"
)

func TestIncompatible(t *testing.T) {
	servers := []string{}

	_, err := filterAndOrderServers(servers, []string{"tcp"})
	if err == nil || err.Error() != "failed to find any compatible servers: " {
		t.Errorf("expected a different error: %v", err)
	}

	for _, a := range []string{"127.0.0.1", "127.0.0.2"} {
		addr := naming.FormatEndpoint("tcp", a, version.IPCVersionRange{100, 200})
		name := naming.JoinAddressName(addr, "")
		servers = append(servers, name)
	}

	_, err = filterAndOrderServers(servers, []string{"tcp"})
	if err == nil || (!strings.HasPrefix(err.Error(), "failed to find any compatible servers:") && !strings.Contains(err.Error(), "No compatible IPC versions available")) {
		vlog.Infof("A: %t . %t", strings.HasPrefix(err.Error(), "failed to find any compatible servers:"), !strings.Contains(err.Error(), "No compatible IPC versions available"))
		t.Errorf("expected a different error to: %v", err)
	}

	for _, a := range []string{"127.0.0.3", "127.0.0.4"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("tcp", a), "")
		servers = append(servers, name)
	}

	_, err = filterAndOrderServers(servers, []string{"foobar"})
	if err == nil || !strings.HasPrefix(err.Error(), "failed to find any servers compatible with [foobar] ") {
		t.Errorf("expected a different error to: %v", err)
	}

}

func TestOrderingByProtocol(t *testing.T) {
	servers := []string{}
	for _, a := range []string{"127.0.0.3", "127.0.0.4"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("tcp", a), "")
		servers = append(servers, name)
	}
	for _, a := range []string{"127.0.0.1", "127.0.0.2"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("tcp4", a), "")
		servers = append(servers, name)
	}
	for _, a := range []string{"127.0.0.10", "127.0.0.11"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("foobar", a), "")
		servers = append(servers, name)
	}
	for _, a := range []string{"127.0.0.7", "127.0.0.8"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("tcp6", a), "")
		servers = append(servers, name)
	}

	got, err := filterAndOrderServers(servers, []string{"batman"})
	if err == nil {
		t.Fatalf("expected an error")
	}

	got, err = filterAndOrderServers(servers, []string{"foobar", "tcp4"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// Just foobar and tcp4
	want := []string{
		"/@3@foobar@127.0.0.10@00000000000000000000000000000000@@@m@@",
		"/@3@foobar@127.0.0.11@00000000000000000000000000000000@@@m@@",
		"/@3@tcp4@127.0.0.1@00000000000000000000000000000000@@@m@@",
		"/@3@tcp4@127.0.0.2@00000000000000000000000000000000@@@m@@",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got: %v, want %v", got, want)
	}

	// Everything, since we didn't specify a protocol, but ordered by
	// the internal metric - see defaultPreferredProtocolOrder.
	// The order will be the default preferred order for protocols, the
	// original ordering within each protocol, with protocols that
	// are not in the default ordering list at the end.
	got, err = filterAndOrderServers(servers, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want = []string{
		"/@3@tcp4@127.0.0.1@00000000000000000000000000000000@@@m@@",
		"/@3@tcp4@127.0.0.2@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@127.0.0.3@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@127.0.0.4@00000000000000000000000000000000@@@m@@",
		"/@3@tcp6@127.0.0.7@00000000000000000000000000000000@@@m@@",
		"/@3@tcp6@127.0.0.8@00000000000000000000000000000000@@@m@@",
		"/@3@foobar@127.0.0.10@00000000000000000000000000000000@@@m@@",
		"/@3@foobar@127.0.0.11@00000000000000000000000000000000@@@m@@",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got: %v, want %v", got, want)
	}

	got, err = filterAndOrderServers(servers, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got: %v, want %v", got, want)
	}

	got, err = filterAndOrderServers(servers, []string{"tcp"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// tcp or tcp4
	want = []string{
		"/@3@tcp4@127.0.0.1@00000000000000000000000000000000@@@m@@",
		"/@3@tcp4@127.0.0.2@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@127.0.0.3@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@127.0.0.4@00000000000000000000000000000000@@@m@@",
	}

	// Ask for all protocols, with no ordering, except for locality
	servers = []string{}
	for _, a := range []string{"74.125.69.139", "127.0.0.3", "127.0.0.1", "192.168.1.10", "74.125.142.83"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("tcp", a), "")
		servers = append(servers, name)
	}
	for _, a := range []string{"127.0.0.10", "127.0.0.11"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("foobar", a), "")
		servers = append(servers, name)
	}
	// Everything, since we didn't specify a protocol
	got, err = filterAndOrderServers(servers, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	want = []string{
		"/@3@tcp@127.0.0.3@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@127.0.0.1@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@74.125.69.139@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@192.168.1.10@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@74.125.142.83@00000000000000000000000000000000@@@m@@",
		"/@3@foobar@127.0.0.10@00000000000000000000000000000000@@@m@@",
		"/@3@foobar@127.0.0.11@00000000000000000000000000000000@@@m@@",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got: %v, want %v", got, want)
	}
}

func TestOrderByNetwork(t *testing.T) {
	servers := []string{}
	for _, a := range []string{"74.125.69.139", "127.0.0.3", "127.0.0.1", "192.168.1.10", "74.125.142.83"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("tcp", a), "")
		servers = append(servers, name)
	}
	got, err := filterAndOrderServers(servers, []string{"tcp"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	want := []string{
		"/@3@tcp@127.0.0.3@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@127.0.0.1@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@74.125.69.139@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@192.168.1.10@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@74.125.142.83@00000000000000000000000000000000@@@m@@",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got: %v, want %v", got, want)
	}
	for _, a := range []string{"74.125.69.139", "127.0.0.3:123", "127.0.0.1", "192.168.1.10", "74.125.142.83"} {
		name := naming.JoinAddressName(naming.FormatEndpoint("ws", a), "")
		servers = append(servers, name)
	}

	got, err = filterAndOrderServers(servers, []string{"ws", "tcp"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	want = []string{
		"/@3@ws@127.0.0.3:123@00000000000000000000000000000000@@@m@@",
		"/@3@ws@127.0.0.1@00000000000000000000000000000000@@@m@@",
		"/@3@ws@74.125.69.139@00000000000000000000000000000000@@@m@@",
		"/@3@ws@192.168.1.10@00000000000000000000000000000000@@@m@@",
		"/@3@ws@74.125.142.83@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@127.0.0.3@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@127.0.0.1@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@74.125.69.139@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@192.168.1.10@00000000000000000000000000000000@@@m@@",
		"/@3@tcp@74.125.142.83@00000000000000000000000000000000@@@m@@",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got: %v, want %v", got, want)
	}
}

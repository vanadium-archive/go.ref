package naming

import (
	"net"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/ipc/version"
	"veyron.io/veyron/veyron2/naming"
)

func TestEndpoint(t *testing.T) {
	v1 := &Endpoint{
		Protocol:     "tcp",
		Address:      "batman.com:1234",
		RID:          naming.FixedRoutingID(0xdabbad00),
		IsMountTable: true,
	}
	v2 := &Endpoint{
		Protocol:      "tcp",
		Address:       "batman.com:2345",
		RID:           naming.FixedRoutingID(0xdabbad00),
		MinIPCVersion: 1,
		MaxIPCVersion: 10,
		IsMountTable:  true,
	}
	v2hp := &Endpoint{
		Protocol:      "tcp",
		Address:       "batman.com:2345",
		RID:           naming.FixedRoutingID(0x0),
		MinIPCVersion: 2,
		MaxIPCVersion: 3,
		IsMountTable:  true,
	}
	v3s := &Endpoint{
		Protocol:      "tcp",
		Address:       "batman.com:2345",
		RID:           naming.FixedRoutingID(0x0),
		MinIPCVersion: 2,
		MaxIPCVersion: 3,
		IsMountTable:  false,
	}
	v3m := &Endpoint{
		Protocol:      "tcp",
		Address:       "batman.com:2345",
		RID:           naming.FixedRoutingID(0xdabbad00),
		MinIPCVersion: 2,
		MaxIPCVersion: 3,
		IsMountTable:  true,
	}

	testcasesA := []struct {
		endpoint naming.Endpoint
		address  string
	}{
		{v1, "batman.com:1234"},
		{v2, "batman.com:2345"},
		{v2hp, "batman.com:2345"},
	}
	for _, test := range testcasesA {
		addr := test.endpoint.Addr()
		if addr.Network() != "tcp" {
			t.Errorf("unexpected network %q", addr.Network())
		}
		if addr.String() != test.address {
			t.Errorf("unexpected address %q, not %q", addr.String(), test.address)
		}
	}

	// Test v3 endpoints.
	defaultVersion = 3
	testcasesC := []struct {
		Endpoint naming.Endpoint
		String   string
		Input    string
		min, max version.IPCVersion
		servesMT bool
	}{
		{v3s, "@3@tcp@batman.com:2345@00000000000000000000000000000000@2@3@s@@", "", 2, 3, false},
		{v3m, "@3@tcp@batman.com:2345@000000000000000000000000dabbad00@2@3@m@@", "", 2, 3, true},
	}

	for _, test := range testcasesC {
		if got, want := test.Endpoint.String(), test.String; got != want {
			t.Errorf("Got %q want %q for endpoint %T = %#v", got, want, test.Endpoint, test.Endpoint)
		}
		str := test.Input
		var ep naming.Endpoint
		var err error
		if str == "" {
			str = test.String
			ep, err = NewEndpoint(str)
		} else {
			ep, err = NewEndpoint(naming.FormatEndpoint("tcp", str,
				version.IPCVersionRange{test.min, test.max},
				naming.ServesMountTableOpt(test.servesMT)))
		}
		if err != nil {
			t.Errorf("Endpoint(%q) failed with %v", str, err)
			continue
		}
		if !reflect.DeepEqual(ep, test.Endpoint) {
			t.Errorf("Got endpoint %T = %#v, want %T = %#v for string %q", ep, ep, test.Endpoint, test.Endpoint, str)
		}
	}

	// Make sure we can continue to parse and create v2 endpoints.
	defaultVersion = 2
	testcasesB := []struct {
		Endpoint naming.Endpoint
		String   string
		Input    string
		min, max version.IPCVersion
		servesMT bool
	}{
		{v1, "@2@tcp@batman.com:1234@000000000000000000000000dabbad00@@@@", "", version.UnknownIPCVersion, version.UnknownIPCVersion, true},
		{v2, "@2@tcp@batman.com:2345@000000000000000000000000dabbad00@1@10@@", "", 1, 10, true},
		{v2hp, "@2@tcp@batman.com:2345@00000000000000000000000000000000@2@3@@", "batman.com:2345", 2, 3, true},
	}

	for _, test := range testcasesB {
		if got, want := test.Endpoint.String(), test.String; got != want {
			t.Errorf("Got %q want %q for endpoint %T = %#v", got, want, test.Endpoint, test.Endpoint)
		}
		str := test.Input
		var ep naming.Endpoint
		var err error
		if str == "" {
			str = test.String
			ep, err = NewEndpoint(str)
		} else {
			ep, err = NewEndpoint(naming.FormatEndpoint("tcp", str,
				version.IPCVersionRange{test.min, test.max},
				naming.ServesMountTableOpt(test.servesMT)))
		}
		if err != nil {
			t.Errorf("Endpoint(%q) failed with %v", str, err)
			continue
		}
		if !reflect.DeepEqual(ep, test.Endpoint) {
			t.Errorf("Got endpoint %T = %#v, want %T = %#v for string %q", ep, ep, test.Endpoint, test.Endpoint, str)
		}
	}
	defaultVersion = 3
}

type endpointTest struct {
	input, output string
	err           error
}

func TestEndpointDefaults(t *testing.T) {
	testcases := []endpointTest{
		{"@1@tcp@batman@@@", "@3@tcp@batman@00000000000000000000000000000000@@@m@@", nil},
		{"@2@tcp@robin@@@@@", "@3@tcp@robin@00000000000000000000000000000000@@@m@@", nil},
		{"@1@@@@@", "@3@tcp@:0@00000000000000000000000000000000@@@m@@", nil},
		{"@2@@@@@@@", "@3@tcp@:0@00000000000000000000000000000000@@@m@@", nil},
		{"@1@tcp@batman:12@@@", "@3@tcp@batman:12@00000000000000000000000000000000@@@m@@", nil},
		{"@host:10@@", "@3@tcp@host:10@00000000000000000000000000000000@@@m@@", nil},
		{"@2@tcp@foo:12@@9@@@", "@3@tcp@foo:12@00000000000000000000000000000000@9@@m@@", nil},
		{"@2@tcp@foo:12@@@4@@", "@3@tcp@foo:12@00000000000000000000000000000000@@4@m@@", nil},
		{"@2@tcp@foo:12@@2@4@@", "@3@tcp@foo:12@00000000000000000000000000000000@2@4@m@@", nil},
		{"@3@@host:11@@@@m@@", "@3@tcp@host:11@00000000000000000000000000000000@@@m@@", nil},
		{"@3@@host:12@@@@@@", "@3@tcp@host:12@00000000000000000000000000000000@@@m@@", nil},
	}
	runEndpointTests(t, testcases)
}

func runEndpointTests(t *testing.T, testcases []endpointTest) {
	for _, test := range testcases {
		ep, err := NewEndpoint(test.input)
		if err == nil && test.err == nil && ep.String() != test.output {
			t.Errorf("NewEndpoint(%q): unexpected endpoint string %q != %q",
				test.input, ep.String(), test.output)
			continue
		}
		switch {
		case test.err == err: // do nothing
		case test.err == nil && err != nil:
			t.Errorf("NewEndpoint(%q): unexpected error %q", test.output, err)
		case test.err != nil && err == nil:
			t.Errorf("NewEndpoint(%q): missing error %q", test.output, test.err)
		case err.Error() != test.err.Error():
			t.Errorf("NewEndpoint(%q): unexpected error  %q != %q", test.output, err, test.err)
		}
	}
}

func TestHostPortEndpoint(t *testing.T) {
	testcases := []endpointTest{
		{"localhost:10", "@3@tcp@localhost:10@00000000000000000000000000000000@@@m@@", nil},
		{"localhost:", "@3@tcp@localhost:@00000000000000000000000000000000@@@m@@", nil},
		{"localhost", "", errInvalidEndpointString},
	}
	runEndpointTests(t, testcases)
}

func TestParseHostPort(t *testing.T) {
	var min, max version.IPCVersion = 1, 2
	dns := &Endpoint{
		Protocol:      "tcp",
		Address:       "batman.com:4444",
		MinIPCVersion: min,
		MaxIPCVersion: max,
		IsMountTable:  true,
	}
	ipv4 := &Endpoint{
		Protocol:      "tcp",
		Address:       "192.168.1.1:4444",
		MinIPCVersion: min,
		MaxIPCVersion: max,
		IsMountTable:  true,
	}
	ipv6 := &Endpoint{
		Protocol:      "tcp",
		Address:       "[01:02::]:4444",
		MinIPCVersion: min,
		MaxIPCVersion: max,
		IsMountTable:  true,
	}
	testcases := []struct {
		Endpoint   naming.Endpoint
		Host, Port string
	}{
		{dns, "batman.com", "4444"},
		{ipv4, "192.168.1.1", "4444"},
		{ipv6, "01:02::", "4444"},
	}

	for _, test := range testcases {
		addr := net.JoinHostPort(test.Host, test.Port)
		epString := naming.FormatEndpoint("tcp", addr, version.IPCVersionRange{min, max})
		if ep, err := NewEndpoint(epString); err != nil {
			t.Errorf("NewEndpoint(%q) failed with %v", addr, err)
		} else {
			if !reflect.DeepEqual(test.Endpoint, ep) {
				t.Errorf("Got endpoint %T = %#v, want %T = %#v for string %q", ep, ep, test.Endpoint, test.Endpoint, addr)
			}
		}
	}
}

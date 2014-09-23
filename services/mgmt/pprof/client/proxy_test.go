package client_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"veyron.io/veyron/veyron/services/mgmt/pprof/client"
	"veyron.io/veyron/veyron/services/mgmt/pprof/impl"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
)

type dispatcher struct {
	invoker ipc.Invoker
}

func (d *dispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	return d.invoker, nil, nil
}

func TestPProfProxy(t *testing.T) {
	r := rt.Init()
	defer r.Cleanup()

	s, err := r.NewServer()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer s.Stop()
	endpoint, err := s.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	if err := s.Serve("", &dispatcher{impl.NewInvoker()}); err != nil {
		t.Fatalf("failed to serve: %v", err)
	}
	l, err := client.StartProxy(r, naming.JoinAddressName(endpoint.String(), ""))
	if err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	defer l.Close()

	testcases := []string{
		"/pprof/",
		"/pprof/cmdline",
		"/pprof/profile?seconds=1",
		"/pprof/heap",
		"/pprof/goroutine",
		fmt.Sprintf("/pprof/symbol?%#x", TestPProfProxy),
	}
	for _, c := range testcases {
		url := "http://" + l.Addr().String() + c
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("http.Get failed: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("unexpected status code. Got %d, want 200", resp.StatusCode)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		resp.Body.Close()
		if len(body) == 0 {
			t.Errorf("unexpected empty body")
		}
	}
}

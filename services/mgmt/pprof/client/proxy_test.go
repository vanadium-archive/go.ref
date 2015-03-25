// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"v.io/v23"
	"v.io/v23/security"

	_ "v.io/x/ref/profiles"
	"v.io/x/ref/services/mgmt/pprof/client"
	"v.io/x/ref/services/mgmt/pprof/impl"
	"v.io/x/ref/test"
)

type dispatcher struct {
	server interface{}
}

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return d.server, nil, nil
}

func TestPProfProxy(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	s, err := v23.NewServer(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer s.Stop()
	endpoints, err := s.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	if err := s.ServeDispatcher("", &dispatcher{impl.NewPProfService()}); err != nil {
		t.Fatalf("failed to serve: %v", err)
	}
	l, err := client.StartProxy(ctx, endpoints[0].Name())
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

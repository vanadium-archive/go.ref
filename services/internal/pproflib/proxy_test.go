// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pproflib_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/internal/pproflib"
	"v.io/x/ref/test"
)

type dispatcher struct {
	server interface{}
}

func (d *dispatcher) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
	return d.server, nil, nil
}

func TestPProfProxy(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	ctx, s, err := v23.WithNewDispatchingServer(ctx, "", &dispatcher{pproflib.NewPProfService()})
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	endpoints := s.Status().Endpoints
	l, err := pproflib.StartProxy(ctx, endpoints[0].Name())
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
		fmt.Sprintf("/pprof/symbol?%p", TestPProfProxy),
	}
	for _, c := range testcases {
		url := "http://" + l.Addr().String() + c
		t.Log(url)
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

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import (
	"io"
	"testing"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"

	"v.io/x/ref/runtime/internal/rpc/stream"
	"v.io/x/ref/runtime/internal/rpc/stream/manager"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

const (
	// Shorthands
	securityNone    = options.SecurityNone
	securityDefault = options.SecurityConfidential
)

type listener struct {
	ln stream.Listener
	ep naming.Endpoint
}

// createListeners returns N (stream.Listener, naming.Endpoint) pairs, such
// that calling stream.Manager.Dial to each of the endpoints will end up
// creating a new VIF.
func createListeners(ctx *context.T, mode options.SecurityLevel, m stream.Manager, N int) (servers []listener, err error) {
	for i := 0; i < N; i++ {
		var (
			l         listener
			principal security.Principal
			blessings security.Blessings
		)
		if mode == securityDefault {
			principal = testutil.NewPrincipal("test")
			blessings = principal.BlessingStore().Default()
		}
		if l.ln, l.ep, err = m.Listen(ctx, "tcp", "127.0.0.1:0", blessings); err != nil {
			return
		}
		servers = append(servers, l)
	}
	return
}

func benchmarkFlow(b *testing.B, mode options.SecurityLevel, nVIFs, nVCsPerVIF, nFlowsPerVC int) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	client := manager.InternalNew(ctx, naming.FixedRoutingID(0xcccccccc))
	server := manager.InternalNew(ctx, naming.FixedRoutingID(0x55555555))

	var principal security.Principal
	if mode == securityDefault {
		principal = testutil.NewPrincipal("test")
		ctx, _ = v23.WithPrincipal(ctx, principal)
	}

	lns, err := createListeners(ctx, mode, server, nVIFs)
	if err != nil {
		b.Fatal(err)
	}

	nFlows := nVIFs * nVCsPerVIF * nFlowsPerVC
	rchan := make(chan io.ReadCloser, nFlows)
	wchan := make(chan io.WriteCloser, nFlows)

	b.ResetTimer()

	go func() {
		defer close(wchan)
		for i := 0; i < nVIFs; i++ {
			ep := lns[i].ep
			for j := 0; j < nVCsPerVIF; j++ {
				vc, err := client.Dial(ctx, ep)
				if err != nil {
					b.Error(err)
					return
				}
				for k := 0; k < nFlowsPerVC; k++ {
					flow, err := vc.Connect()
					if err != nil {
						b.Error(err)
						return
					}
					// Flows are "Accepted" by the remote
					// end only on the first Write.
					if _, err := flow.Write([]byte("hello")); err != nil {
						b.Error(err)
						return
					}
					wchan <- flow
				}
			}
		}
	}()

	go func() {
		defer close(rchan)
		for i := 0; i < nVIFs; i++ {
			ln := lns[i].ln
			nFlowsPerVIF := nVCsPerVIF * nFlowsPerVC
			for j := 0; j < nFlowsPerVIF; j++ {
				flow, err := ln.Accept()
				if err != nil {
					b.Error(err)
					return
				}
				rchan <- flow
			}
		}
	}()

	var readers []io.ReadCloser
	for r := range rchan {
		readers = append(readers, r)
	}
	var writers []io.WriteCloser
	for w := range wchan {
		writers = append(writers, w)
	}
	if b.Failed() {
		return
	}
	(&throughputTester{b: b, readers: readers, writers: writers}).Run()
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"bytes"
	"crypto/rand"
	"io"
	"sync"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/flow"
	"v.io/v23/rpc/version"
	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/runtime/internal/flow/flowtest"
	"v.io/x/ref/test/goroutines"
)

const leakWaitTime = 250 * time.Millisecond

var randData []byte

func init() {
	randData = make([]byte, 2*DefaultBytesBufferedPerFlow)
	if _, err := rand.Read(randData); err != nil {
		panic("Could not read random data.")
	}
}

func trunc(b []byte) []byte {
	if len(b) > 100 {
		return b[:100]
	}
	return b
}

func doWrite(t *testing.T, f flow.Flow, data []byte) {
	mid := len(data) / 2
	wrote, err := f.WriteMsg(data[:mid], data[mid:])
	if err != nil || wrote != len(data) {
		t.Errorf("Unexpected result for write: %d, %v wanted %d, nil", wrote, err, len(data))
	}
}

func doRead(t *testing.T, f flow.Flow, want []byte, wg *sync.WaitGroup) {
	for read := 0; len(want) > 0; read++ {
		got, err := f.ReadMsg()
		if err != nil && err != io.EOF {
			t.Errorf("Unexpected error: %v", err)
			break
		}
		if !bytes.Equal(got, want[:len(got)]) {
			t.Errorf("On read %d got: %v want %v", read, trunc(got), trunc(want))
			break
		}
		want = want[len(got):]
	}
	if len(want) != 0 {
		t.Errorf("got %d leftover bytes, expected 0.", len(want))
	}
	wg.Done()
}

func TestLargeWrite(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	df, flows, cl := setupFlow(t, "local", "", ctx, ctx, true)
	defer cl()
	defer shutdown()

	var wg sync.WaitGroup
	wg.Add(2)
	go doWrite(t, df, randData)
	go doRead(t, df, randData, &wg)
	af := <-flows
	go doRead(t, af, randData, &wg)
	go doWrite(t, af, randData)
	wg.Wait()
}

func TestUpdateFlowHandler(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()

	ctx, shutdown := v23.Init()
	dmrw, amrw := flowtest.Pipe(t, ctx, "local", "")
	versions := version.RPCVersionRange{Min: 3, Max: 5}
	ep, err := v23.NewEndpoint("@6@@batman.com:1234@@000000000000000000000000dabbad00@m@@@")
	if err != nil {
		t.Fatal(err)
	}
	dch, ach := make(chan *Conn), make(chan *Conn)
	q1, q2 := make(chan flow.Flow, 1), make(chan flow.Flow, 1)
	fh1, fh2 := fh(q1), fh(q2)
	lBlessings := v23.GetPrincipal(ctx).BlessingStore().Default()
	go func() {
		d, err := NewDialed(ctx, lBlessings, dmrw, ep, ep, versions, flowtest.AllowAllPeersAuthorizer{}, time.Minute, nil)
		if err != nil {
			panic(err)
		}
		dch <- d
	}()
	go func() {
		a, err := NewAccepted(ctx, lBlessings, nil, amrw, ep, versions, time.Minute, fh1)
		if err != nil {
			panic(err)
		}
		ach <- a
	}()
	d, a := <-dch, <-ach
	var f flow.Flow
	if f, err = d.Dial(ctx, flowtest.AllowAllPeersAuthorizer{}, nil); err != nil {
		t.Fatal(err)
	}
	// Write a byte to send the openFlow message.
	if _, err := f.Write([]byte{'a'}); err != nil {
		t.Fatal(err)
	}
	// The flow should be accepted in fh1.
	<-q1
	// After updating to fh2 the flow should be accepted in fh2.
	a.UpdateFlowHandler(ctx, fh2)
	if f, err = d.Dial(ctx, flowtest.AllowAllPeersAuthorizer{}, nil); err != nil {
		t.Fatal(err)
	}
	// Write a byte to send the openFlow message.
	if _, err := f.Write([]byte{'a'}); err != nil {
		t.Fatal(err)
	}
	<-q2
	shutdown()
	d.Close(ctx, nil)
	a.Close(ctx, nil)
}

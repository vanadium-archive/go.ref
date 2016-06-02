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

	"v.io/v23/flow"
	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/test"
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
	ctx, shutdown := test.V23Init()
	defer shutdown()
	df, flows, cl := setupFlow(t, "local", "", ctx, ctx, true)
	defer cl()

	var wg sync.WaitGroup
	wg.Add(2)
	go doWrite(t, df, randData)
	go doRead(t, df, randData, &wg)
	af := <-flows
	go doRead(t, af, randData, &wg)
	go doWrite(t, af, randData)
	wg.Wait()
}

func TestConnRTT(t *testing.T) {
	defer goroutines.NoLeaks(t, leakWaitTime)()
	payload := []byte{0}
	ctx, shutdown := test.V23Init()
	defer shutdown()
	df, flows, cl := setupFlow(t, "local", "", ctx, ctx, true)
	defer cl()

	go doWrite(t, df, payload)
	af := <-flows

	if df.Conn().RTT() == 0 {
		t.Errorf("dialed conn's RTT should be non-zero")
	}
	if af.Conn().RTT() == 0 {
		t.Errorf("accepted conn's RTT should be non-zero")
	}
}

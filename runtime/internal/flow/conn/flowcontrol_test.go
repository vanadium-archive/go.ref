// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/flow/message"
	_ "v.io/x/ref/runtime/factories/fake"
	"v.io/x/ref/runtime/internal/flow/flowtest"
)

func block(c *Conn, p int) chan struct{} {
	w := &singleMessageWriter{writeCh: make(chan struct{}), p: p}
	w.next, w.prev = w, w
	ready, unblock := make(chan struct{}), make(chan struct{})
	c.mu.Lock()
	c.activateWriterLocked(w)
	c.notifyNextWriterLocked(w)
	c.mu.Unlock()
	go func() {
		<-w.writeCh
		close(ready)
		<-unblock
		c.mu.Lock()
		c.deactivateWriterLocked(w)
		c.notifyNextWriterLocked(w)
		c.mu.Unlock()
	}()
	<-ready
	return unblock
}

func forkForRead(conn *Conn) *flowtest.MRW {
	return conn.mp.rw.(*flowtest.MRW).ForkForRead()
}

func waitForWriters(ctx *context.T, conn *Conn, num int) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for _ = range t.C {
		conn.mu.Lock()
		count := 0
		for _, w := range conn.activeWriters {
			if w != nil {
				count++
				for _, n := w.neighbors(); n != w; _, n = n.neighbors() {
					count++
				}
			}
		}
		conn.mu.Unlock()
		if count >= num {
			return
		}
	}
}

func TestOrdering(t *testing.T) {
	const nflows = 5
	const nmessages = 5

	ctx, shutdown := v23.Init()
	defer shutdown()

	flows, accept, dc, ac := setupFlows(t, ctx, ctx, true, nflows, true)

	unblock := block(dc, 0)
	fork := forkForRead(ac)
	var wg sync.WaitGroup
	wg.Add(2 * nflows)
	defer wg.Wait()
	for _, f := range flows {
		go func(fl flow.Flow) {
			if _, err := fl.WriteMsg(randData[:mtu*nmessages]); err != nil {
				panic(err)
			}
			wg.Done()
		}(f)
		go func() {
			fl := <-accept
			buf := make([]byte, mtu*nmessages)
			if _, err := io.ReadFull(fl, buf); err != nil {
				panic(err)
			}
			if !bytes.Equal(buf, randData[:mtu*nmessages]) {
				t.Fatal("unequal data")
			}
			wg.Done()
		}()
	}
	waitForWriters(ctx, dc, nflows+1)
	// Now close the flow which will send a teardown message, but only after
	// the other flows finish their current write.
	go dc.Close(ctx, nil)
	defer func() { <-dc.Closed(); <-ac.Closed() }()

	close(unblock)

	// OK now we expect all the flows to write interleaved messages.
	for i := 0; i < nmessages; i++ {
		found := map[uint64]bool{}
		for j := 0; j < nflows; j++ {
			s, err := fork.ReadMsg()
			if err != nil {
				t.Fatal(err)
			}
			m, err := message.Read(ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			switch msg := m.(type) {
			case *message.OpenFlow:
				found[msg.ID] = true
			case *message.Data:
				found[msg.ID] = true
			default:
				t.Fatalf("Unexpected message %#v", m)
			}
		}
		if len(found) != nflows {
			t.Fatalf("Did not recieve a message from each flow in round %d: %v", i, found)
		}
	}
	// expect the teardown message last
	s, err := fork.ReadMsg()
	if err != nil {
		t.Fatal(err)
	}
	m, err := message.Read(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*message.TearDown); !ok {
		t.Errorf("expected teardown got %#v", m)
	}
}

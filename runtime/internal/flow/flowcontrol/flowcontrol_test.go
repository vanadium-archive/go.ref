// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flowcontrol

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/ref/test"
)

var testdata = make([]byte, 1<<20)

func init() {
	test.Init()
	_, err := io.ReadFull(rand.Reader, testdata)
	if err != nil {
		panic(err)
	}
}

func TestFlowControl(t *testing.T) {
	const (
		workers  = 10
		messages = 10
	)

	msgs := make(map[int][]byte)
	fc := New(256, 64)

	ctx, cancel := context.RootContext()
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			el := fc.NewWorker(fmt.Sprintf("%d", idx), 0)
			go el.Release(ctx, messages*5) // Try to make races happen
			j := 0
			el.Run(ctx, func(tokens int) (used int, done bool, err error) {
				msgs[idx] = append(msgs[idx], []byte(fmt.Sprintf("%d-%d,", idx, j))...)
				j++
				return 3, j >= messages, nil
			})
			wg.Done()
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		buf := &bytes.Buffer{}
		for j := 0; j < messages; j++ {
			fmt.Fprintf(buf, "%d-%d,", i, j)
		}
		if want, got := buf.String(), string(msgs[i]); want != got {
			t.Errorf("Got %s, want %s for %d", got, want, i)
		}
	}
}

func expect(t *testing.T, work chan interface{}, values ...interface{}) {
	for i, w := range values {
		if got := <-work; got != w {
			t.Errorf("expected %p in pos %d got %p", w, i, got)
		}
	}
}

func TestOrdering(t *testing.T) {
	const mtu = 10

	ctx, cancel := context.RootContext()
	defer cancel()
	fc := New(0, mtu)

	work := make(chan interface{})
	worker := func(p int) *Worker {
		w := fc.NewWorker(fmt.Sprintf("%d", p), p)
		go w.Run(ctx, func(t int) (int, bool, error) {
			work <- w
			return t, false, nil
		})
		w.Release(ctx, mtu)
		<-work
		return w
	}

	w0 := worker(0)
	w1a := worker(1)
	w1b := worker(1)
	w1c := worker(1)
	w2 := worker(2)

	// Release to all the flows at once and ensure the writes
	// happen in the correct order.
	fc.Release(ctx, []Release{{w0, 2 * mtu}, {w1a, 2 * mtu}, {w1b, 3 * mtu}, {w1c, 0}, {w2, mtu}})
	expect(t, work, w0, w0, w1a, w1b, w1a, w1b, w1b, w2)
}

func TestSharedCounters(t *testing.T) {
	const (
		mtu    = 10
		shared = 2 * mtu
	)

	ctx, cancel := context.RootContext()
	defer cancel()

	fc := New(shared, mtu)

	work := make(chan interface{})

	worker := func(p int) *Worker {
		w := fc.NewWorker(fmt.Sprintf("%d", p), p)
		go w.Run(ctx, func(t int) (int, bool, error) {
			work <- w
			return t, false, nil
		})
		return w
	}

	// w0 should run twice on shared counters.
	w0 := worker(0)
	expect(t, work, w0, w0)

	w1 := worker(1)
	// Now Release to w0 which shouldn't allow it to run since it's just repaying, but
	// should allow w1 to run on the returned shared counters.
	w0.Release(ctx, 2*mtu)
	expect(t, work, w1, w1)

	// Releasing again will allow w0 to run.
	w0.Release(ctx, mtu)
	expect(t, work, w0)
}

func TestConcurrentRun(t *testing.T) {
	ctx, cancel := context.RootContext()
	defer cancel()
	const mtu = 10
	fc := New(mtu, mtu)

	ready, wait := make(chan struct{}), make(chan struct{})
	w := fc.NewWorker("", 0)
	go w.Run(ctx, func(t int) (int, bool, error) {
		close(ready)
		<-wait
		return t, true, nil
	})
	<-ready
	if err := w.Run(ctx, nil); verror.ErrorID(err) != ErrConcurrentRun.ID {
		t.Errorf("expected concurrent run error got: %v", err)
	}
	close(wait)
}

func TestNonFlowControlledRun(t *testing.T) {
	ctx, cancel := context.RootContext()
	defer cancel()
	const mtu = 10
	fc := New(0, mtu)

	work := make(chan interface{})
	ready, wait := make(chan struct{}), make(chan struct{})
	// Start one worker running
	go fc.Run(ctx, "0", 0, func(t int) (int, bool, error) {
		close(ready)
		<-wait
		return t, true, nil
	})
	<-ready
	// Now queue up sever workers and make sure they execute in order.
	go fc.Run(ctx, "2", 2, func(t int) (int, bool, error) {
		work <- "c"
		return t, true, nil
	})
	go fc.Run(ctx, "1", 1, func(t int) (int, bool, error) {
		work <- "b"
		return t, true, nil
	})
	go fc.Run(ctx, "0", 0, func(t int) (int, bool, error) {
		work <- "a"
		return t, true, nil
	})
	for fc.numActive() < 4 {
		time.Sleep(time.Millisecond)
	}
	close(wait)
	expect(t, work, "a", "b", "c")
}

func newNullConn(mtu int) net.Conn {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	addr := ln.Addr()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		ln.Close()
		buf := make([]byte, mtu)
		for {
			_, err := conn.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				panic(err)
			}
		}
		conn.Close()
	}()

	conn, err := net.Dial(addr.Network(), addr.String())
	if err != nil {
		panic(err)
	}
	return conn
}

func BenchmarkWithFlowControl(b *testing.B) {
	const (
		mtu     = 1 << 16
		shared  = 1 << 20
		workers = 100
	)
	ctx, cancel := context.RootContext()
	defer cancel()
	s := newNullConn(mtu)

	for n := 0; n < b.N; n++ {
		fc := New(shared, mtu)
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func(idx int) {
				w := fc.NewWorker(fmt.Sprintf("%d", idx), 0)
				w.Release(ctx, len(testdata))
				t := testdata
				err := w.Run(ctx, func(tokens int) (used int, done bool, err error) {
					towrite := min(tokens, len(t))
					written, err := s.Write(t[:min(tokens, len(t))])
					t = t[written:]
					return towrite, len(t) == 0, err
				})
				if err != nil {
					panic(err)
				}
				wg.Done()
			}(i)
		}
		wg.Wait()
	}
	if err := s.Close(); err != nil {
		panic(err)
	}
}

func BenchmarkWithoutFlowControl(b *testing.B) {
	const (
		workers = 100
		mtu     = 1 << 16
	)
	s := newNullConn(mtu)
	for n := 0; n < b.N; n++ {
		for cursor := 0; cursor < len(testdata); cursor += mtu {
			for i := 0; i < workers; i++ {
				_, err := s.Write(testdata[cursor : cursor+mtu])
				if err != nil {
					panic(err)
				}
			}
		}
	}
	if err := s.Close(); err != nil {
		panic(err)
	}
}

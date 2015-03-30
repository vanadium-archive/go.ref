// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mocknet_test

import (
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"v.io/x/ref/profiles/internal/testing/mocks/mocknet"
)

//go:generate v23 test generate

func newListener(t *testing.T, opts mocknet.Opts) net.Listener {
	ln, err := mocknet.ListenerWithOpts(opts, "test", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func TestTrace(t *testing.T) {
	opts := mocknet.Opts{
		Mode: mocknet.Trace,
		Tx:   make(chan int, 100),
		Rx:   make(chan int, 100),
	}
	ln := newListener(t, opts)
	defer ln.Close()

	var rxconn net.Conn
	var rxerr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		rxconn, rxerr = ln.Accept()
		wg.Done()
	}()

	txconn, err := mocknet.DialerWithOpts(opts, "test", ln.Addr().String(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	rw := func(s string) {
		b := make([]byte, len(s))
		txconn.Write([]byte(s))
		rxconn.Read(b[:])
		if got, want := string(b), s; got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	sizes := []int{}
	for _, s := range []string{"hello", " ", "world"} {
		rw(s)
		sizes = append(sizes, len(s))
	}
	rxconn.Close()
	close(opts.Tx)
	close(opts.Rx)
	sizes = append(sizes, -1)

	drain := func(ch chan int) []int {
		r := []int{}
		for v := range ch {
			r = append(r, v)
		}
		return r
	}

	if got, want := drain(opts.Rx), sizes; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := drain(opts.Tx), sizes; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestClose(t *testing.T) {
	cases := []struct {
		txClose, rxClose int
		tx               []string
		rx               []string
		err              error
	}{
		{6, 10, []string{"hello", "world"}, []string{"hello", "w"}, io.EOF},
		{5, 10, []string{"hello", "world"}, []string{"hello", ""}, io.EOF},
		{8, 6, []string{"hello", "world"}, []string{"hello", "w"}, io.EOF},
		{8, 5, []string{"hello", "world"}, []string{"hello", ""}, errors.New("use of closed network connection")},
	}

	for ci, c := range cases {
		opts := mocknet.Opts{
			Mode:      mocknet.Close,
			TxCloseAt: c.txClose,
			RxCloseAt: c.rxClose,
		}

		ln := newListener(t, opts)
		defer ln.Close()

		var rxconn net.Conn
		var rxerr error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			rxconn, rxerr = ln.Accept()
			wg.Done()
		}()

		txconn, err := mocknet.DialerWithOpts(opts, "test", ln.Addr().String(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		wg.Wait()

		rw := func(s string) (int, int, string, error) {
			b := make([]byte, len(s))
			tx, _ := txconn.Write([]byte(s))
			rx, err := rxconn.Read(b[:])
			return tx, rx, string(b[0:rx]), err
		}

		txBytes := 0
		rxBytes := 0
		for i, m := range c.tx {
			tx, rx, rxed, err := rw(m)
			if got, want := rxed, c.rx[i]; got != want {
				t.Fatalf("%d: got %q, want %q", ci, got, want)
			}
			txBytes += tx
			rxBytes += rx
			if err != nil {
				if got, want := err.Error(), c.err.Error(); got != want {
					t.Fatalf("%d: got %v, want %v", ci, got, want)
				}
			}
		}
		if got, want := txBytes, c.txClose; got != want {
			t.Fatalf("%d: got %v, want %v", ci, got, want)
		}
		rxWant := c.rxClose
		if rxWant > c.txClose {
			rxWant = c.txClose
		}
		if got, want := rxBytes, rxWant; got != want {
			t.Fatalf("%d: got %v, want %v", ci, got, want)

		}
	}
}

func TestDrop(t *testing.T) {
	cases := []struct {
		txDropAfter int
		tx          []string
		rx          []string
	}{
		{6, []string{"hello", "world"}, []string{"hello", "w"}},
		{2, []string{"hello", "world"}, []string{"he", "wo"}},
		{0, []string{"hello", "world"}, []string{"", ""}},
	}

	for ci, c := range cases {
		opts := mocknet.Opts{
			Mode:        mocknet.Drop,
			TxDropAfter: func() int { return c.txDropAfter },
		}

		ln := newListener(t, opts)
		defer ln.Close()

		var rxconn net.Conn
		var rxerr error
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			rxconn, rxerr = ln.Accept()
			wg.Done()
		}()

		txconn, err := mocknet.DialerWithOpts(opts, "test", ln.Addr().String(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		wg.Wait()

		rw := func(s string, l int) (int, int, string, error) {
			b := make([]byte, l)
			tx, _ := txconn.Write([]byte(s))
			rx, err := rxconn.Read(b[:])
			return tx, rx, string(b[0:rx]), err
		}

		for i, m := range c.tx {
			tx, rx, rxed, _ := rw(m, len(c.rx[i]))
			if got, want := rxed, c.rx[i]; got != want {
				t.Fatalf("%d: got %q, want %q", ci, got, want)
			}
			if tx != rx {
				t.Fatalf("%d: tx %d, rx %d", ci, tx, rx)
			}
		}
	}
}

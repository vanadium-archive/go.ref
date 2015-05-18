// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"

	"v.io/x/ref/runtime/internal/lib/iobuf"
)

const (
	text = `Lorem ipsum dolor sit amet, consectetur adipisicing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.`
)

func min(i, j int) int {
	if i < j {
		return i
	}
	return j
}

// testControlCipher is a super-simple cipher that xor's each byte of the
// payload with 0xaa.
type testControlCipher struct{}

const testMACSize = 4

func (*testControlCipher) MACSize() int {
	return testMACSize
}

func testMAC(data []byte) []byte {
	var h uint32
	for _, b := range data {
		h = (h << 1) ^ uint32(b)
	}
	var hash [4]byte
	binary.BigEndian.PutUint32(hash[:], h)
	return hash[:]
}

func (c *testControlCipher) Decrypt(data []byte) {
	for i, _ := range data {
		data[i] ^= 0xaa
	}
}

func (c *testControlCipher) Encrypt(data []byte) {
	for i, _ := range data {
		data[i] ^= 0xaa
	}
}

func (c *testControlCipher) Open(data []byte) bool {
	mac := testMAC(data[:len(data)-testMACSize])
	if bytes.Compare(mac, data[len(data)-testMACSize:]) != 0 {
		return false
	}
	c.Decrypt(data[:len(data)-testMACSize])
	return true
}

func (c *testControlCipher) Seal(data []byte) error {
	c.Encrypt(data[:len(data)-testMACSize])
	mac := testMAC(data[:len(data)-testMACSize])
	copy(data[len(data)-testMACSize:], mac)
	return nil
}

func (c *testControlCipher) ChannelBinding() []byte { return nil }

// shortConn performs at most 3 bytes of IO at a time.
type shortConn struct {
	io.ReadWriteCloser
}

func (s *shortConn) Read(data []byte) (int, error) {
	if len(data) > 3 {
		data = data[:3]
	}
	return s.ReadWriteCloser.Read(data)
}

func (s *shortConn) Write(data []byte) (int, error) {
	n := len(data)
	for i := 0; i < n; i += 3 {
		j := min(n, i+3)
		m, err := s.ReadWriteCloser.Write(data[i:j])
		if err != nil {
			return i + m, err
		}
	}
	return n, nil
}

func TestConn(t *testing.T) {
	p1, p2 := net.Pipe()
	pool := iobuf.NewPool(0)
	r1 := iobuf.NewReader(pool, p1)
	r2 := iobuf.NewReader(pool, p2)
	f1 := newSetupConn(p1, r1, &testControlCipher{})
	f2 := newSetupConn(p2, r2, &testControlCipher{})
	testConn(t, f1, f2)
}

func TestShortInnerConn(t *testing.T) {
	p1, p2 := net.Pipe()
	s1 := &shortConn{p1}
	s2 := &shortConn{p2}
	pool := iobuf.NewPool(0)
	r1 := iobuf.NewReader(pool, s1)
	r2 := iobuf.NewReader(pool, s2)
	f1 := newSetupConn(s1, r1, &testControlCipher{})
	f2 := newSetupConn(s2, r2, &testControlCipher{})
	testConn(t, f1, f2)
}

func TestShortOuterConn(t *testing.T) {
	p1, p2 := net.Pipe()
	pool := iobuf.NewPool(0)
	r1 := iobuf.NewReader(pool, p1)
	r2 := iobuf.NewReader(pool, p2)
	e1 := newSetupConn(p1, r1, &testControlCipher{})
	e2 := newSetupConn(p2, r2, &testControlCipher{})
	f1 := &shortConn{e1}
	f2 := &shortConn{e2}
	testConn(t, f1, f2)
}

// Write prefixes of the text onto the framed pipe and verify the frame content.
func testConn(t *testing.T, f1, f2 io.ReadWriteCloser) {
	// Reader loop.
	var pending sync.WaitGroup
	pending.Add(1)
	go func() {
		var buf [1024]byte
		for i := 1; i != len(text); i++ {
			n, err := io.ReadFull(f1, buf[:i])
			if err != nil {
				t.Errorf("bad read: %s", err)
			}
			if n != i {
				t.Errorf("bad read: got %d bytes, expected %d bytes", n, i)
			}
			actual := string(buf[:n])
			expected := string(text[:n])
			if actual != expected {
				t.Errorf("got %q, expected %q", actual, expected)
			}
		}
		pending.Done()
	}()

	// Writer.
	for i := 1; i != len(text); i++ {
		if n, err := f2.Write([]byte(text[:i])); err != nil || n != i {
			t.Errorf("bad write: i=%d n=%d err=%s", i, n, err)
		}
	}
	pending.Wait()
}

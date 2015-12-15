// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package websocket_test

import (
	"encoding/gob"
	"fmt"
	"hash/crc64"
	"io"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/rpc"
)

var crcTable *crc64.Table

func init() {
	crcTable = crc64.MakeTable(crc64.ISO)
}

func newSender(t *testing.T, dialer rpc.DialerFunc, protocol, address string) net.Conn {
	ctx, _ := context.RootContext()
	conn, err := dialer(ctx, protocol, address, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
		return nil
	}
	return conn
}

func checkProtocols(conn net.Conn, tx string) error {
	expectedProtocol := map[string]string{
		"ws": "ws", "wsh": "tcp", "tcp": "tcp",
	}
	if got, want := conn.LocalAddr().Network(), expectedProtocol[tx]; got != want {
		return fmt.Errorf("wrong local protocol: got %q, want %q", got, want)
	}
	// Can't tell that the remote protocol is really 'wsh'
	if got, want := conn.RemoteAddr().Network(), expectedProtocol[tx]; got != want {
		return fmt.Errorf("wrong remote protocol: got %q, want %q", got, want)
	}
	return nil
}

type packet struct {
	Data  []byte
	Size  int
	CRC64 uint64
}

func createPacket() *packet {
	p := &packet{}
	p.Size = rand.Intn(4 * 1024)
	p.Data = make([]byte, p.Size)
	for i := 0; i < p.Size; i++ {
		p.Data[i] = byte(rand.Int() & 0xff)
	}
	p.CRC64 = crc64.Checksum([]byte(p.Data), crcTable)
	return p
}

func checkPacket(p *packet) error {
	if got, want := len(p.Data), p.Size; got != want {
		return fmt.Errorf("wrong sizes: got %d, want %d", got, want)
	}
	crc := crc64.Checksum(p.Data, crcTable)
	if got, want := crc, p.CRC64; got != want {
		return fmt.Errorf("wrong crc: got %d, want %d", got, want)
	}
	return nil
}

type backChannel struct {
	crcChan  chan uint64
	byteChan chan []byte
	errChan  chan error
}

type bcTable struct {
	ready *sync.Cond
	sync.Mutex
	bc map[string]*backChannel
}

var globalBCTable bcTable

func init() {
	globalBCTable.ready = sync.NewCond(&globalBCTable)
	globalBCTable.bc = make(map[string]*backChannel)
}

func (bt *bcTable) waitfor(key string) *backChannel {
	bt.Lock()
	defer bt.Unlock()
	for {
		bc := bt.bc[key]
		if bc != nil {
			delete(bt.bc, key)
			return bc
		}
		bt.ready.Wait()
	}
}

func (bt *bcTable) add(key string, bc *backChannel) {
	bt.Lock()
	bt.bc[key] = bc
	bt.Unlock()
	bt.ready.Broadcast()
}

func packetReceiver(t *testing.T, ln net.Listener, bc *backChannel) {
	conn, err := ln.Accept()
	if err != nil {
		close(bc.crcChan)
		close(bc.errChan)
		return
	}

	globalBCTable.add(conn.RemoteAddr().String(), bc)

	defer conn.Close()
	dec := gob.NewDecoder(conn)
	rxed := 0
	for {
		var p packet
		err := dec.Decode(&p)
		if err != nil {
			if err != io.EOF {
				bc.errChan <- fmt.Errorf("unexpected error: %s", err)
			}
			close(bc.crcChan)
			close(bc.errChan)
			return
		}
		if err := checkPacket(&p); err != nil {
			bc.errChan <- fmt.Errorf("unexpected error: %s", err)
		}
		bc.crcChan <- p.CRC64
		rxed++
	}
}

func packetSender(t *testing.T, nPackets int, conn net.Conn) {
	txCRCs := make([]uint64, nPackets)
	enc := gob.NewEncoder(conn)
	for i := 0; i < nPackets; i++ {
		p := createPacket()
		txCRCs[i] = p.CRC64
		if err := enc.Encode(p); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}
	conn.Close() // Close the connection so that the receiver quits.

	bc := globalBCTable.waitfor(conn.LocalAddr().String())
	for err := range bc.errChan {
		if err != nil {
			t.Fatalf(err.Error())
		}
	}

	rxed := 0
	for rxCRC := range bc.crcChan {
		if got, want := rxCRC, txCRCs[rxed]; got != want {
			t.Errorf("%s -> %s: packet %d: mismatched CRCs: got %d, want %d", conn.LocalAddr().String(), conn.RemoteAddr().String(), rxed, got, want)
		}
		rxed++
	}
	if got, want := rxed, nPackets; got != want {
		t.Fatalf("%s -> %s: got %d, want %d", conn.LocalAddr().String(), conn.RemoteAddr().String(), got, want)
	}
}

func packetRunner(t *testing.T, ln net.Listener, dialer rpc.DialerFunc, protocol, address string) {
	nPackets := 100
	go packetReceiver(t, ln, &backChannel{
		crcChan: make(chan uint64, nPackets),
		errChan: make(chan error, nPackets),
	})

	conn := newSender(t, dialer, protocol, address)
	if err := checkProtocols(conn, protocol); err != nil {
		t.Fatalf(err.Error())
	}
	packetSender(t, nPackets, conn)
}

func byteReceiver(t *testing.T, ln net.Listener, bc *backChannel) {
	conn, err := ln.Accept()
	if err != nil {
		close(bc.byteChan)
		close(bc.errChan)
		return
	}
	globalBCTable.add(conn.RemoteAddr().String(), bc)

	defer conn.Close()
	rxed := 0
	for {
		buf := make([]byte, rxed+1)
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				bc.errChan <- fmt.Errorf("unexpected error: %s", err)
			}
			close(bc.byteChan)
			close(bc.errChan)
			return
		}
		if got, want := n, len(buf[:n]); got != want {
			bc.errChan <- fmt.Errorf("%s -> %s: got %d bytes, expected %d", conn.LocalAddr().String(), conn.RemoteAddr().String(), got, want)
		}
		if got, want := buf[0], byte(0xff); got != want {
			bc.errChan <- fmt.Errorf("%s -> %s: got %x, want %x", conn.LocalAddr().String(), conn.RemoteAddr().String(), got, want)
		}
		bc.byteChan <- buf[:n]
		rxed++
	}
}

func byteSender(t *testing.T, nIterations int, conn net.Conn) {
	txBytes := make([][]byte, nIterations+1)
	for i := 0; i < nIterations; i++ {
		p := make([]byte, i+1)
		p[0] = 0xff
		for j := 1; j <= i; j++ {
			p[j] = byte(64 + i) // start at ASCII A
		}
		txBytes[i] = p
		n, err := conn.Write(p)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got, want := n, i+1; got != want {
			t.Fatalf("wrote %d, not %d bytes", got, want)
		}
	}
	conn.Close()

	bc := globalBCTable.waitfor(conn.LocalAddr().String())

	for err := range bc.errChan {
		if err != nil {
			t.Fatalf(err.Error())
		}
	}

	addr := fmt.Sprintf("%s -> %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
	rxed := 0
	for rxBytes := range bc.byteChan {
		if got, want := len(rxBytes), rxed+1; got != want {
			t.Fatalf("%s: got %d, want %d bytes", addr, got, want)
		}
		if got, want := rxBytes[0], byte(0xff); got != want {
			t.Fatalf("%s: got %x, want %x", addr, got, want)
		}
		for i := 0; i < len(rxBytes); i++ {
			if got, want := rxBytes[i], txBytes[rxed][i]; got != want {
				t.Fatalf("%s: got %c, want %c", addr, got, want)
			}
		}
		rxed++
	}
	if got, want := rxed, nIterations; got != want {
		t.Fatalf("%s: got %d, want %d", addr, got, want)
	}
}

func byteRunner(t *testing.T, ln net.Listener, dialer rpc.DialerFunc, protocol, address string) {
	nIterations := 10
	go byteReceiver(t, ln, &backChannel{
		byteChan: make(chan []byte, nIterations),
		errChan:  make(chan error, nIterations),
	})

	conn := newSender(t, dialer, protocol, address)
	defer conn.Close()
	if err := checkProtocols(conn, protocol); err != nil {
		t.Fatalf(err.Error())
	}
	byteSender(t, nIterations, conn)
}

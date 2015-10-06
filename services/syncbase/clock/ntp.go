// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"fmt"
	"net"
	"time"

	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
)

const (
	udp  = "udp"
	port = "123"
)

var _ NtpSource = (*ntpSourceImpl)(nil)

func NewNtpSource(clock SystemClock) NtpSource {
	return &ntpSourceImpl{util.NtpServerPool, clock}
}

type ntpSourceImpl struct {
	ntpHost string
	sc      SystemClock
}

// NtpSync samples data from NTP server and returns the one which has the lowest
// network delay. The sample with lowest network delay will have the least error
// in computation of the offset.
// Param sampleCount is the number of samples this method will fetch.
func (ns *ntpSourceImpl) NtpSync(sampleCount int) (*NtpData, error) {
	var canonicalSample *NtpData = nil
	for i := 0; i < sampleCount; i++ {
		if sample, err := ns.sample(); err == nil {
			if (canonicalSample == nil) || (sample.delay < canonicalSample.delay) {
				canonicalSample = sample
			}
		}
	}
	if canonicalSample == nil {
		err := fmt.Errorf("clock: NtpSync: Failed to get any sample from NTP server: %s", ns.ntpHost)
		return nil, err
	}
	return canonicalSample, nil
}

// Sample connects to an NTP server and returns NtpData containing the clock
// offset and the network delay experienced while talking to the server.
//
// NTP protocol involves sending a request of size 48 bytes with the first
// byte containing protocol version and mode and the last 8 bytes containing
// transmit timestamp. The current NTP version is 4. A response from NTP server
// contains original timestamp (client's transmit timestamp from request) from
// bytes 24 to 31, server's receive timestamp from byte 32 to 39 and server's
// transmit time from byte 40 to 47. Client can register the response receive
// time as soon it receives a response from server.
// Based on the 4 timestamps the client can compute the offset between the
// two clocks and the roundtrip network delay for the request.
func (ns *ntpSourceImpl) sample() (*NtpData, error) {
	raddr, err := net.ResolveUDPAddr(udp, ns.ntpHost+":"+port)
	if err != nil {
		return nil, err
	}

	con, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}
	defer con.Close()

	// To make sure that the system clock does not change between fetching
	// send and receive timestamps, we get the elapsed time since
	// boot (which is immutable) before registering the send timestamp and
	// after registering the receive timestamp and call HasSysClockChanged()
	// to verify if the clock changed in between or not. If it did, we return
	// ErrInternal as response.
	elapsedOrig, err := ns.sc.ElapsedTime()
	if err != nil {
		vlog.Errorf("clock: NtpSync: error while fetching elapsed time: %v", err)
		return nil, err
	}

	msg := ns.createRequest()
	_, err = con.Write(msg)
	if err != nil {
		return nil, err
	}

	con.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = con.Read(msg)
	if err != nil {
		return nil, err
	}

	clientReceiveTs := ns.sc.Now()
	elapsedEnd, err := ns.sc.ElapsedTime()
	if err != nil {
		vlog.Errorf("clock: NtpSync: error while fetching elapsed time: %v", err)
		return nil, err
	}

	clientTransmitTs := extractTime(msg[24:32])
	serverReceiveTs := extractTime(msg[32:40])
	serverTransmitTs := extractTime(msg[40:48])

	if HasSysClockChanged(clientTransmitTs, clientReceiveTs, elapsedOrig, elapsedEnd) {
		err := fmt.Errorf("clock: NtpSync: system clock changed midway through syncing wih NTP.")
		vlog.Errorf("%v", err)
		return nil, err
	}

	// Following code extracts the clock offset and network delay based on the
	// transmit and receive timestamps on the client and the server as per
	// the formula explained at http://www.eecis.udel.edu/~mills/time.html
	data := NtpData{}
	data.offset = (serverReceiveTs.Sub(clientTransmitTs) + serverTransmitTs.Sub(clientReceiveTs)) / 2
	data.delay = clientReceiveTs.Sub(clientTransmitTs) - serverTransmitTs.Sub(serverReceiveTs)
	data.ntpTs = serverTransmitTs
	return &data, nil
}

func (ns *ntpSourceImpl) createRequest() []byte {
	data := make([]byte, 48)
	data[0] = 0x23 // protocol version = 4, mode = 3 (Client)

	// For NTP the prime epoch, or base date of era 0, is 0 h 1 January 1900 UTC
	t0 := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	tnow := ns.sc.Now()
	d := tnow.Sub(t0)
	nsec := d.Nanoseconds()

	// The encoding of timestamp below is an exact opposite of the decoding
	// being done in extractTime(). Refer extractTime() for more explaination.
	sec := nsec / 1e9                  // Integer part of seconds since epoch
	frac := ((nsec % 1e9) << 32) / 1e9 // fractional part of seconds since epoch

	// write the timestamp to Transmit Timestamp section of request.
	data[43] = byte(sec)
	data[42] = byte(sec >> 8)
	data[41] = byte(sec >> 16)
	data[40] = byte(sec >> 24)

	data[47] = byte(frac)
	data[46] = byte(frac >> 8)
	data[45] = byte(frac >> 16)
	data[44] = byte(frac >> 24)
	return data
}

// ExtractTime takes a byte array which contains encoded timestamp from NTP
// server starting at the 0th byte and is 8 bytes long. The encoded timestamp is
// in seconds since 1900. The first 4 bytes contain the integer part of of the
// seconds while the last 4 bytes contain the fractional part of the seconds
// where (FFFFFFFF + 1) represents 1 second while 00000001 represents 2^(-32) of
// a second.
func extractTime(data []byte) time.Time {
	var sec, frac uint64
	sec = uint64(data[3]) | uint64(data[2])<<8 | uint64(data[1])<<16 | uint64(data[0])<<24
	frac = uint64(data[7]) | uint64(data[6])<<8 | uint64(data[5])<<16 | uint64(data[4])<<24

	// multiply the integral second part with 1Billion to convert to nanoseconds
	nsec := sec * 1e9
	// multiply frac part with 2^(-32) to get the correct value in seconds and
	// then multiply with 1Billion to convert to nanoseconds. The multiply by
	// Billion is done first to make sure that we dont loose precision.
	nsec += (frac * 1e9) >> 32

	t := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(nsec)).Local()

	return t
}

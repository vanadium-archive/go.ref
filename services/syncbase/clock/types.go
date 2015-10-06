// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"time"
)

// This interface provides a wrapper over system clock to allow easy testing
// of VClock and other code that uses timestamps.
type SystemClock interface {
	// Now returns the current UTC time as known by the system.
	// This may not reflect the NTP time if the system clock is out of
	// sync with NTP.
	Now() time.Time

	// ElapsedTime returns a duration representing the time elapsed since the device
	// rebooted.
	ElapsedTime() (time.Duration, error)
}

type NtpSource interface {
	// NtpSync obtains NtpData samples from an NTP server and returns the one
	// which has the lowest network delay.
	// Param sampleCount is the number of samples this method will fetch.
	// NtpData contains the clock offset and the network delay experienced while
	// talking to the server.
	NtpSync(sampleCount int) (*NtpData, error)
}

type NtpData struct {
	// Offset is the difference between the NTP time and the system clock.
	// Adding offset to system clock will give estimated NTP time.
	offset time.Duration

	// Delay is the round trip network delay experienced while talking to NTP
	// server. The smaller the delay, the more accurate the offset is.
	delay time.Duration

	// Timestamp from NTP server.
	ntpTs time.Time
}

// PeerSyncData contains information about the peer's clock along with the
// timestamps related to the roundtrip.
type PeerSyncData struct {
	// The send timestamp received in TimeReq from the originator.
	MySendTs time.Time
	// Time when the request was received by peer.
	RecvTs time.Time
	// Time when the response was sent by peer.
	SendTs time.Time
	// Time when the response was received by the originator.
	MyRecvTs time.Time
	// Timestamp received from NTP during last NTP sync. The last NTP sync could
	// be done either by this device or some other device that this device
	// synced its clock with.
	LastNtpTs *time.Time
	// Number of reboots since last NTP sync.
	NumReboots uint16
	// Number of hops between this device and the device that did the last
	// NTP sync.
	NumHops uint16
}

func (cd *ClockData) SystemBootTime() time.Time {
	ns := time.Second.Nanoseconds()
	return time.Unix(cd.SystemTimeAtBoot/ns, cd.SystemTimeAtBoot%ns)
}

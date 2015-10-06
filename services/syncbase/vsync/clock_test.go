// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"testing"
	"time"

	"v.io/x/ref/services/syncbase/clock"
	"v.io/x/ref/services/syncbase/server/interfaces"
)

func TestClockGetTime(t *testing.T) {
	service := createService(t)
	defer destroyService(t, service)

	ntpTs := time.Now().Add(-10 * time.Minute)
	skew := 3 * time.Second

	clockData := newClockData(skew.Nanoseconds(), &ntpTs, 0, 0)
	vclock := clock.NewVClock(service.St())
	tx := vclock.St().NewTransaction()
	if err := vclock.SetClockData(tx, clockData); err != nil {
		t.Errorf("Failed to store clock data with error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	req := createReq(time.Now())
	resp, err := service.Sync().GetTime(nil, nil, req, "test")
	if err != nil {
		t.Errorf("GetTime rpc returned error: %v", err)
		t.FailNow()
	}
	offset := getOffset(resp)

	if abs(offset-skew) > time.Millisecond {
		t.Errorf("GetTime returned a skew beyond error margin. expected: %v, actual: %v", skew, offset)
	}
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func createReq(ts time.Time) interfaces.TimeReq {
	return interfaces.TimeReq{
		SendTs: ts,
	}
}

func newClockData(skew int64, ntp *time.Time, reboots, hops uint16) *clock.ClockData {
	return &clock.ClockData{
		SystemTimeAtBoot:     0,
		Skew:                 skew,
		ElapsedTimeSinceBoot: 0,
		LastNtpTs:            ntp,
		NumReboots:           reboots,
		NumHops:              hops,
	}
}

func getOffset(resp interfaces.TimeResp) time.Duration {
	clientReceiveTs := time.Now()
	clientTransmitTs := resp.OrigTs
	serverReceiveTs := resp.RecvTs
	serverTransmitTs := resp.SendTs
	return (serverReceiveTs.Sub(clientTransmitTs) + serverTransmitTs.Sub(clientReceiveTs)) / 2
}

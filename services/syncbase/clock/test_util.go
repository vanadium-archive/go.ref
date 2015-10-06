// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

// Utilities for testing clock.

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

/////////////////////////////////////////////////
// Mock for SystemClock

var _ SystemClock = (*systemClockMockImpl)(nil)

func MockSystemClock(now time.Time, elapsedTime time.Duration) *systemClockMockImpl {
	return &systemClockMockImpl{
		now:         now,
		elapsedTime: elapsedTime,
	}
}

type systemClockMockImpl struct {
	now         time.Time
	elapsedTime time.Duration
}

func (sc *systemClockMockImpl) Now() time.Time {
	return sc.now
}

func (sc *systemClockMockImpl) SetNow(now time.Time) {
	sc.now = now
}

func (sc *systemClockMockImpl) ElapsedTime() (time.Duration, error) {
	return sc.elapsedTime, nil
}

func (sc *systemClockMockImpl) SetElapsedTime(elapsed time.Duration) {
	sc.elapsedTime = elapsed
}

/////////////////////////////////////////////////
// Mock for NtpSource

var _ NtpSource = (*ntpSourceMockImpl)(nil)

func MockNtpSource() *ntpSourceMockImpl {
	return &ntpSourceMockImpl{}
}

type ntpSourceMockImpl struct {
	Err  error
	Data *NtpData
}

func (ns *ntpSourceMockImpl) NtpSync(sampleCount int) (*NtpData, error) {
	if ns.Err != nil {
		return nil, ns.Err
	}
	return ns.Data, nil
}

func NewVClockWithMockServices(st store.Store, sc SystemClock, ns NtpSource) *VClock {
	if sc == nil {
		sc = newSystemClock()
	}
	if ns == nil {
		ns = NewNtpSource(sc)
	}
	return &VClock{
		SysClock:  sc,
		ntpSource: ns,
		st:        st,
	}
}

//////////////////////////////////////////////////
// Utility functions

func VerifyClockData(t *testing.T, vclock *VClock, expected *ClockData) {
	// verify ClockData
	clockData := &ClockData{}
	if err := vclock.GetClockData(vclock.St(), clockData); err != nil {
		t.Errorf("Expected to find clockData, found error: %v", err)
	}

	if clockData.Skew != expected.Skew {
		t.Errorf("Expected value for skew: %d, found: %d", expected.Skew, clockData.Skew)
	}
	if clockData.ElapsedTimeSinceBoot != expected.ElapsedTimeSinceBoot {
		t.Errorf("Expected value for elapsed time: %d, found: %d", expected.ElapsedTimeSinceBoot,
			clockData.ElapsedTimeSinceBoot)
	}
	if clockData.SystemTimeAtBoot != expected.SystemTimeAtBoot {
		t.Errorf("Expected value for SystemTimeAtBoot: %d, found: %d",
			expected.SystemTimeAtBoot, clockData.SystemTimeAtBoot)
	}
	exNtp := expected.LastNtpTs
	actNtp := clockData.LastNtpTs
	if !timeEquals(exNtp, actNtp) {
		t.Errorf("Expected value for LastNtpTs: %v, found: %v",
			fmtTime(expected.LastNtpTs), fmtTime(clockData.LastNtpTs))
	}
	if clockData.NumReboots != expected.NumReboots {
		t.Errorf("Expected value for NumReboots: %v, found %v",
			expected.NumReboots, clockData.NumReboots)
	}
	if clockData.NumHops != expected.NumHops {
		t.Errorf("Expected value for NumHops: %v, found %v",
			expected.NumHops, clockData.NumHops)
	}
}

func newClockData(sysBootTime, skew, elapsedTime int64, ntp *time.Time, reboots, hops uint16) *ClockData {
	return &ClockData{
		SystemTimeAtBoot:     sysBootTime,
		Skew:                 skew,
		ElapsedTimeSinceBoot: elapsedTime,
		LastNtpTs:            ntp,
		NumReboots:           reboots,
		NumHops:              hops,
	}
}

func timeEquals(expected, actual *time.Time) bool {
	if expected == actual {
		return true
	}
	if (expected == nil && actual != nil) || (expected != nil && actual == nil) {
		return false
	}
	return expected.Equal(*actual)
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "nil"
	}
	return fmt.Sprintf("%v", *t)
}

type testStore struct {
	st     store.Store
	engine string
	dir    string
}

func createStore(t *testing.T) *testStore {
	engine := "memstore"
	opts := util.OpenOptions{CreateIfMissing: true, ErrorIfExists: false}
	dir := fmt.Sprintf("%s/vclock_test_%d_%d", os.TempDir(), os.Getpid(), time.Now().UnixNano())

	st, err := util.OpenStore(engine, path.Join(dir, engine), opts)
	if err != nil {
		t.Fatalf("cannot create store %s (%s): %v", engine, dir, err)
	}
	return &testStore{st: st, engine: engine, dir: dir}
}

func destroyStore(t *testing.T, s *testStore) {
	if err := util.DestroyStore(s.engine, s.dir); err != nil {
		t.Fatalf("cannot destroy store %s (%s): %v", s.engine, s.dir, err)
	}
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"testing"
	"time"

	"v.io/v23/verror"
)

// ** Test Setup **
// Local: No ClockData
// Remote: Has ClockData but no NTP info
// Result: Clock not synced
func TestClockCheckLocalClock1(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	var remoteDelta time.Duration = 5 * time.Second
	resp := createResp(remoteDelta, nil, 0, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, nil)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	err := vclock.GetClockData(vclock.St(), &ClockData{})
	if verror.ErrorID(err) != verror.ErrNoExist.ID {
		t.Errorf("Expected ErrNoExists but got: %v", err)
	}
}

// ** Test Setup **
// Local: No ClockData
// Remote: Has ClockData with NTP info and acceptable {reboot,hop}
// Result: Clock synced
func TestClockCheckLocalClock2(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	var remoteDelta time.Duration = 5 * time.Second
	remoteNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	resp := createResp(remoteDelta, &remoteNtpTs, 0, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, nil)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	verifyPartialClockData(t, vclock, remoteDelta, remoteNtpTs, 0, 1)
}

// ** Test Setup **
// Local: Has ClockData but no NTP info
// Remote: Has ClockData but no NTP info
// Result: Clock not synced
func TestClockCheckLocalClock3(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 3 * time.Second
	clockData := newSyncClockData(localSkew.Nanoseconds(), nil, 0, 0)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 5 * time.Second
	resp := createResp(remoteDelta, nil, 0, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	VerifyClockData(t, vclock, clockData)
}

// ** Test Setup **
// Local: Has ClockData but no NTP info
// Remote: Has ClockData with NTP info and acceptable {reboot,hop}
// Result: Clock synced
func TestClockCheckLocalClock4(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 3 * time.Second
	clockData := newSyncClockData(localSkew.Nanoseconds(), nil, 0, 0)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 5 * time.Second
	remoteNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	resp := createResp(remoteDelta, &remoteNtpTs, 0, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	verifyPartialClockData(t, vclock, remoteDelta+localSkew, remoteNtpTs, 0, 1)
}

// ** Test Setup **
// Local: Has ClockData with NTP info
// Remote: Has ClockData but no NTP info
// Result: Clock not synced
func TestClockCheckLocalClock5(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 3 * time.Second
	localNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	clockData := newSyncClockData(localSkew.Nanoseconds(), &localNtpTs, 0, 0)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 5 * time.Second
	resp := createResp(remoteDelta, nil, 0, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	VerifyClockData(t, vclock, clockData)
}

// ** Test Setup **
// Local & Remote have ClockData with NTP info.
// LocalNtp > RemoteNtp, acceptable values for {reboot,hop}
// Result: Clock not synced
func TestClockCheckLocalClock6(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 3 * time.Second
	localNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	clockData := newSyncClockData(localSkew.Nanoseconds(), &localNtpTs, 1, 1)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 5 * time.Second
	remoteNtpTs := localNtpTs.Add(-10 * time.Minute).UTC()
	resp := createResp(remoteDelta, &remoteNtpTs, 0, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	VerifyClockData(t, vclock, clockData)
}

// ** Test Setup **
// Local & Remote have ClockData with NTP info.
// LocalNtp < RemoteNtp, Remote-reboot = 0, Remote-hop = 1
// Result: Clock synced, skew = oldSkew + offset, numReboots = 0, numHops = 2
func TestClockCheckLocalClock7(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 5 * time.Second
	localNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	clockData := newSyncClockData(localSkew.Nanoseconds(), &localNtpTs, 0, 0)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 8 * time.Second
	remoteNtpTs := localNtpTs.Add(10 * time.Minute).UTC()
	resp := createResp(remoteDelta, &remoteNtpTs, 0, 1)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	verifyPartialClockData(t, vclock, remoteDelta+localSkew, remoteNtpTs, 0, 2)
}

// ** Test Setup **
// Local & Remote have ClockData with NTP info.
// LocalNtp < RemoteNtp, unacceptable value for hop
// Result: Clock not synced
func TestClockCheckLocalClock8(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 3 * time.Second
	localNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	clockData := newSyncClockData(localSkew.Nanoseconds(), &localNtpTs, 1, 1)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 5 * time.Second
	remoteNtpTs := localNtpTs.Add(10 * time.Minute).UTC()
	resp := createResp(remoteDelta, &remoteNtpTs, 0, 2)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	VerifyClockData(t, vclock, clockData)
}

// ** Test Setup **
// Local & Remote have ClockData with NTP info.
// LocalNtp < RemoteNtp, Remote-reboots > 0 but Diff between two clocks < 1 minute
// Result: Clock synced, skew = oldSkew + offset, numReboots = remote reboots, numHops = 1
func TestClockCheckLocalClock9(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 3 * time.Second
	localNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	clockData := newSyncClockData(localSkew.Nanoseconds(), &localNtpTs, 0, 0)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 5 * time.Second
	remoteNtpTs := localNtpTs.Add(10 * time.Minute).UTC()
	resp := createResp(remoteDelta, &remoteNtpTs, 3, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	verifyPartialClockData(t, vclock, remoteDelta+localSkew, remoteNtpTs, 3, 1)
}

// ** Test Setup **
// Local & Remote have ClockData with NTP info.
// LocalNtp < RemoteNtp, Remote-reboots > 0 and Diff between two clocks > 1 minute
// Result: Clock not synced
func TestClockCheckLocalClock10(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 3 * time.Second
	localNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	clockData := newSyncClockData(localSkew.Nanoseconds(), &localNtpTs, 0, 0)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 5 * time.Hour
	remoteNtpTs := localNtpTs.Add(10 * time.Minute).UTC()
	resp := createResp(remoteDelta, &remoteNtpTs, 3, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	VerifyClockData(t, vclock, clockData)
}

// ** Test Setup **
// Local & Remote have ClockData with NTP info.
// LocalNtp < RemoteNtp, but the difference between the two clocks is too small
// Result: Clock not synced
func TestClockCheckLocalClock11(t *testing.T) {
	testStore := createStore(t)
	defer destroyStore(t, testStore)
	vclock := NewVClock(testStore.st)

	localSkew := 3 * time.Second
	localNtpTs := time.Now().Add(-30 * time.Minute).UTC()
	clockData := newSyncClockData(localSkew.Nanoseconds(), &localNtpTs, 1, 1)
	storeClockData(t, vclock, clockData)

	var remoteDelta time.Duration = 1 * time.Second
	remoteNtpTs := localNtpTs.Add(10 * time.Minute).UTC()
	resp := createResp(remoteDelta, &remoteNtpTs, 0, 0)

	tx := vclock.St().NewTransaction()
	vclock.ProcessPeerClockData(tx, resp, clockData)
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}

	VerifyClockData(t, vclock, clockData)
}

func toRemoteTime(t time.Time, remoteDelta time.Duration) time.Time {
	return t.Add(remoteDelta)
}

func toLocalTime(t time.Time, remoteDelta time.Duration) time.Time {
	return t.Add(-remoteDelta)
}

func storeClockData(t *testing.T, c *VClock, clockData *ClockData) {
	tx := c.St().NewTransaction()
	if err := c.SetClockData(tx, clockData); err != nil {
		t.Errorf("Failed to store clock data with error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("Error while commiting tx: %v", err)
	}
}

func fetchClockData(t *testing.T, c *VClock) *ClockData {
	clockData := &ClockData{}
	if err := c.GetClockData(c.St(), clockData); err != nil {
		t.Errorf("Failed to store clock data with error: %v", err)
	}
	return clockData
}

func newSyncClockData(skew int64, ntp *time.Time, reboots, hops uint16) *ClockData {
	return newClockData(0, skew, 0, ntp, reboots, hops)
}

func createResp(remoteDelta time.Duration, ntpTs *time.Time, reboots, hops uint16) *PeerSyncData {
	oTs := time.Now()
	rTs := toRemoteTime(oTs.Add(time.Second), remoteDelta)
	sTs := rTs.Add(time.Millisecond)
	clientRecvTs := toLocalTime(sTs, remoteDelta).Add(time.Second)
	return &PeerSyncData{
		MySendTs:   oTs,
		RecvTs:     rTs,
		SendTs:     sTs,
		MyRecvTs:   clientRecvTs,
		LastNtpTs:  ntpTs,
		NumReboots: reboots,
		NumHops:    hops,
	}
}

func verifyPartialClockData(t *testing.T, c *VClock, skew time.Duration, remoteNtpTs time.Time, reboots, hops uint16) {
	clockData := fetchClockData(t, c)
	if time.Duration(clockData.Skew) != skew {
		t.Errorf("Value for clock skew expected: %v, actual: %v", skew, time.Duration(clockData.Skew))
	}
	if clockData.LastNtpTs == nil {
		t.Errorf("Clock data LastNtpTs should not be nil")
	}
	if !clockData.LastNtpTs.Equal(remoteNtpTs) {
		t.Errorf("Clock data LastNtpTs expected: %v, actual: %v", remoteNtpTs, clockData.LastNtpTs)
	}
	if clockData.NumReboots != reboots {
		t.Errorf("Clock data NumReboots expected: %v, actual: %v", reboots, clockData.NumReboots)
	}
	if clockData.NumHops != hops {
		t.Errorf("Clock data NumHops expected: %v, actual: %v", hops, clockData.NumHops)
	}
}

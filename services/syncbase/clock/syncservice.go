// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"math"
	"time"

	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// ProcessPeerClockData accepts PeerSyncData and local ClockData and updates
// local ClockData and persists it if peer's clock is more accurate. Peer's
// clock is more accurate if all of the following conditions are met:
// 1) diff between the two clocks is > 2 seconds (avoids constant tweaking).
// 2) peer synced with NTP and it was more recent than local.
// 3) peer has not rebooted since NTP or the peer has rebooted since NTP but
//    the difference btw the two clocks is < 1 minute.
// 4) num hops for peer's NTP sync data is < 2, i.e. either the peer has
//    synced with NTP itself or it synced with another peer that did NTP.
// Retruns true if the local clock was updated.
func (c *VClock) ProcessPeerClockData(tx store.Transaction, resp *PeerSyncData, localData *ClockData) bool {
	offset := (resp.RecvTs.Sub(resp.MySendTs) + resp.SendTs.Sub(resp.MyRecvTs)) / 2
	vlog.VI(2).Infof("clock: ProcessPeerClockData: offset between two clocks: %v", offset)
	if math.Abs(float64(offset.Nanoseconds())) <= util.PeerSyncDiffThreshold {
		vlog.VI(2).Info("clock: ProcessPeerClockData: the two clocks are synced within PeerSyncDiffThreshold.")
		return false
	}
	if resp.LastNtpTs == nil {
		vlog.VI(2).Info("clock: ProcessPeerClockData: peer clock has not synced to NTP. Ignoring peer's clock.")
		return false
	}
	if (localData != nil) && !isPeerNtpSyncMoreRecent(localData.LastNtpTs, resp.LastNtpTs) {
		vlog.VI(2).Info("clock: ProcessPeerClockData: peer NTP sync is less recent than local.")
		return false
	}
	if isOverRebootTolerance(offset, resp.NumReboots) {
		vlog.VI(2).Info("clock: ProcessPeerClockData: peer clock is over reboot tolerance.")
		return false
	}
	if resp.NumHops >= util.HopTolerance {
		vlog.VI(2).Info("clock: ProcessPeerClockData: peer clock is over hop tolerance.")
		return false
	}
	vlog.VI(2).Info("clock: ProcessPeerClockData: peer's clock is more accurate than local clock. Syncing to peer's clock.")
	return c.updateClockData(tx, resp, offset, localData)
}

// updateClockData updates the clock data for the local clock based on peer
// clock's data.
// Returs true if update succeeds.
func (c *VClock) updateClockData(tx store.Transaction, peerResp *PeerSyncData, offset time.Duration, localData *ClockData) bool {
	systemTime := c.SysClock.Now()
	elapsedTime, err := c.SysClock.ElapsedTime()
	if err != nil {
		vlog.Errorf("clock: ProcessPeerClockData: error while fetching elapsed time: %v", err)
		return false
	}
	systemTimeAtBoot := systemTime.Add(-elapsedTime)
	var skew time.Duration
	if localData == nil {
		// localData nil means that the local estimated UTC time is equal to
		// the system time. Hence the offset was in reality between the local
		// system clock and the remote estimated UTC time.
		skew = offset
	} else {
		skew = time.Duration(localData.Skew) + offset
	}

	newClockData := &ClockData{
		SystemTimeAtBoot:     systemTimeAtBoot.UnixNano(),
		Skew:                 skew.Nanoseconds(),
		ElapsedTimeSinceBoot: elapsedTime.Nanoseconds(),
		LastNtpTs:            peerResp.LastNtpTs,
		NumReboots:           peerResp.NumReboots,
		NumHops:              (peerResp.NumHops + 1),
	}
	if err := c.SetClockData(tx, newClockData); err != nil {
		vlog.Errorf("clock: ProcessPeerClockData: error while setting new clock data: %v", err)
		return false
	}
	return true
}

func isPeerNtpSyncMoreRecent(localNtpTs, peerNtpTs *time.Time) bool {
	if localNtpTs == nil {
		return true
	}
	if peerNtpTs == nil {
		return false
	}
	return peerNtpTs.After(*localNtpTs)
}

func isOverRebootTolerance(offset time.Duration, numReboots uint16) bool {
	return (math.Abs(float64(offset.Nanoseconds())) > util.RebootTolerance) && (numReboots > 0)
}

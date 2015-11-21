// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vclock

// This file defines the VClockD struct and methods.

import (
	"sync"
	"time"

	"v.io/x/lib/vlog"
)

// VClockD is a daemon (a goroutine) that periodically runs doLocalUpdate and
// doNtpUpdate to update various fields in persisted VClockData based on values
// reported by the system clock and NTP.
type VClockD struct {
	vclock    *VClock
	ntpSource NtpSource
	// State to coordinate shutdown of spawned goroutines.
	pending sync.WaitGroup
	closed  chan struct{}
}

// NewVClockD returns a new VClockD instance.
func NewVClockD(vclock *VClock) *VClockD {
	return &VClockD{
		vclock:    vclock,
		ntpSource: NewNtpSource(NtpDefaultHost, vclock.SysClock),
		closed:    make(chan struct{}),
	}
}

// Start starts this VClockD's run loop.
func (d *VClockD) Start() {
	d.pending.Add(1)
	go d.runLoop()
}

// Close cleans up any VClockD state and waits for its run loop to exit.
func (d *VClockD) Close() {
	close(d.closed)
	d.pending.Wait()
}

////////////////////////////////////////////////////////////////////////////////
// Internal implementation of VClockD

const (
	localInterval = time.Second
	ntpInterval   = time.Hour
)

// runLoop's ticker ticks on every localInterval, and we run doLocalUpdate on
// every tick. On every ntpMod'th tick, we also run doNtpUpdate.
var ntpMod = (ntpInterval / localInterval).Nanoseconds()

func (d *VClockD) runLoop() {
	vlog.VI(1).Infof("vclockd: runLoop: start")
	defer vlog.VI(1).Infof("vclockd: runLoop: end")
	defer d.pending.Done()

	ticker := time.NewTicker(localInterval)
	defer ticker.Stop()

	var count int64 = 0
	for {
		d.doLocalUpdate()
		if count == 0 {
			d.doNtpUpdate()
		}
		count = (count + 1) % ntpMod
		vlog.VI(5).Infof("vclockd: runLoop: iteration complete")

		select {
		case <-d.closed:
			vlog.VI(1).Infof("vclockd: runLoop: channel closed, exiting")
			return
		case <-ticker.C:
		}

		// Prioritize closed in case both ticker and closed have been triggered.
		select {
		case <-d.closed:
			vlog.VI(1).Infof("vclockd: runLoop: channel closed, exiting")
			return
		default:
		}
	}
}

////////////////////////////////////////
// doLocalUpdate

// doLocalUpdate checks for reboots and drift by comparing our persisted
// VClockData with the current time and elapsed time reported by the system
// clock. It always updates {SystemTimeAtBoot, ElapsedTimeSinceBoot}, and may
// also update either Skew or NumReboots. It does not touch LastNtpTs or
// NumHops.
func (d *VClockD) doLocalUpdate() error {
	vlog.VI(2).Info("vclock: doLocalUpdate: start")
	defer vlog.VI(2).Info("vclock: doLocalUpdate: end")

	err := d.vclock.UpdateVClockData(func(data *VClockData) (*VClockData, error) {
		now := d.vclock.SysClock.Now()
		elapsedTime, err := d.vclock.SysClock.ElapsedTime()
		if err != nil {
			vlog.Errorf("vclock: doLocalUpdate: error fetching elapsed time: %v", err)
			return nil, err
		}

		// Check for a reboot: elapsed time is monotonic, so if the current elapsed
		// time is less than data.ElapsedTimeSinceBoot, a reboot has taken place.
		if elapsedTime < data.ElapsedTimeSinceBoot {
			vlog.VI(2).Info("vclock: doLocalUpdate: detected reboot")
			data.NumReboots += 1
		} else {
			// No reboot detected. Check whether the system clock has drifted
			// substantially, e.g. due to the user (or some other program) changing
			// the vclock time.
			expectedNow := data.SystemTimeAtBoot.Add(elapsedTime)
			delta := expectedNow.Sub(now)
			if abs(delta) > SystemClockDriftThreshold {
				vlog.VI(2).Infof("vclock: doLocalUpdate: detected clock drift of %v; updating SystemTimeAtBoot", delta)
				data.Skew = data.Skew + delta
			}
		}

		// Always update {SystemTimeAtBoot, ElapsedTimeSinceBoot}.
		data.SystemTimeAtBoot = now.Add(-elapsedTime)
		data.ElapsedTimeSinceBoot = elapsedTime
		return data, nil
	})

	if err != nil {
		vlog.Errorf("vclock: doLocalUpdate: update failed: %v", err)
	}
	return err
}

////////////////////////////////////////
// doNtpUpdate

// doNtpUpdate talks to an NTP server and updates VClockData.
func (d *VClockD) doNtpUpdate() error {
	vlog.VI(2).Info("vclock: doNtpUpdate: start")
	defer vlog.VI(2).Info("vclock: doNtpUpdate: end")

	ntpData, err := d.ntpSource.NtpSync(NtpSampleCount)
	if err != nil {
		vlog.Errorf("vclock: doNtpUpdate: failed to fetch NTP time: %v", err)
		return err
	}
	vlog.VI(2).Infof("vclock: doNtpUpdate: NTP skew is %v", ntpData.skew)

	err = d.vclock.UpdateVClockData(func(data *VClockData) (*VClockData, error) {
		now := d.vclock.SysClock.Now()
		elapsedTime, err := d.vclock.SysClock.ElapsedTime()
		if err != nil {
			vlog.Errorf("vclock: doNtpUpdate: error fetching elapsed time: %v", err)
			return nil, err
		}

		// Only update skew if the delta is greater than NtpSkewDeltaThreshold, to
		// avoid constant tweaking of the clock.
		delta := ntpData.skew - data.Skew
		if abs(delta) > NtpSkewDeltaThreshold {
			vlog.VI(2).Infof("vclock: doNtpUpdate: NTP time minus Syncbase vclock time is %v; updating Skew", delta)
			data.Skew = ntpData.skew
		}

		data.SystemTimeAtBoot = now.Add(-elapsedTime)
		data.ElapsedTimeSinceBoot = elapsedTime
		data.LastNtpTs = ntpData.ntpTs
		data.NumReboots = 0
		data.NumHops = 0
		return data, nil
	})

	if err != nil {
		vlog.Errorf("vclock: doNtpUpdate: update failed: %v", err)
	}
	return err
}

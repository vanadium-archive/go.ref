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

// VClockD is a daemon (a goroutine) that periodically runs DoLocalUpdate and
// DoNtpUpdate to update various fields in persisted VClockData based on values
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
		ntpSource: NewNtpSource(NtpDefaultHost, vclock),
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

// Note, DoLocalUpdate and DoNtpUpdate are public so that
// Service.DevModeUpdateClock can call them directly. We considered having
// DevModeUpdateClock schedule updates to happen within runLoop, but this would
// require a Mutex and would force clients to sleep to allow time for the
// requested update to happen.

const (
	localInterval = time.Second
	ntpInterval   = time.Hour
)

// runLoop's ticker ticks on every localInterval, and we run DoLocalUpdate on
// every tick. On every ntpMod'th tick, we also run DoNtpUpdate.
var ntpMod = int64(ntpInterval) / int64(localInterval)

func (d *VClockD) runLoop() {
	vlog.VI(1).Infof("vclockd: runLoop: start")
	defer vlog.VI(1).Infof("vclockd: runLoop: end")
	defer d.pending.Done()

	ticker := time.NewTicker(localInterval)
	defer ticker.Stop()

	var count int64 = 0
	for {
		d.DoLocalUpdate()
		if count == 0 {
			d.DoNtpUpdate()
		}
		count = (count + 1) % ntpMod
		vlog.VI(5).Infof("vclockd: runLoop: iteration complete")

		select {
		case <-d.closed:
			vlog.VI(1).Infof("vclockd: runLoop: channel closed, exiting")
			return
		case <-ticker.C:
		}

		// Prioritize closed in case both ticker and closed have fired.
		select {
		case <-d.closed:
			vlog.VI(1).Infof("vclockd: runLoop: channel closed, exiting")
			return
		default:
		}
	}
}

////////////////////////////////////////
// DoLocalUpdate

// DoLocalUpdate checks for reboots and drift by comparing our persisted
// VClockData with the current time and elapsed time reported by the system
// clock. It always updates {SystemTimeAtBoot, ElapsedTimeSinceBoot}, and may
// also update either Skew or NumReboots. It does not touch LastNtpTs or
// NumHops.
func (d *VClockD) DoLocalUpdate() error {
	vlog.VI(2).Info("vclock: DoLocalUpdate: start")
	defer vlog.VI(2).Info("vclock: DoLocalUpdate: end")

	err := d.vclock.UpdateVClockData(func(data *VClockData) (*VClockData, error) {
		now, elapsedTime, err := d.vclock.SysClockVals()
		if err != nil {
			vlog.Errorf("vclock: DoLocalUpdate: SysClockVals failed: %v", err)
			return nil, err
		}

		// Check for a reboot: elapsed time is monotonic, so if the current elapsed
		// time is less than data.ElapsedTimeSinceBoot, a reboot has taken place.
		if elapsedTime < data.ElapsedTimeSinceBoot {
			vlog.VI(2).Info("vclock: DoLocalUpdate: detected reboot")
			data.NumReboots += 1
		} else {
			// No reboot detected. Check whether the system clock has drifted
			// substantially, e.g. due to the user (or some other program) changing
			// the vclock time.
			expectedNow := data.SystemTimeAtBoot.Add(elapsedTime)
			delta := expectedNow.Sub(now)
			if abs(delta) > SystemClockDriftThreshold {
				vlog.VI(2).Infof("vclock: DoLocalUpdate: detected clock drift of %v; updating SystemTimeAtBoot", delta)
				data.Skew = data.Skew + delta
			}
		}

		// Always update {SystemTimeAtBoot, ElapsedTimeSinceBoot}.
		data.SystemTimeAtBoot = now.Add(-elapsedTime)
		data.ElapsedTimeSinceBoot = elapsedTime
		return data, nil
	})

	if err != nil {
		vlog.Errorf("vclock: DoLocalUpdate: update failed: %v", err)
	}
	return err
}

////////////////////////////////////////
// DoNtpUpdate

// DoNtpUpdate talks to an NTP server and updates VClockData.
func (d *VClockD) DoNtpUpdate() error {
	vlog.VI(2).Info("vclock: DoNtpUpdate: start")
	defer vlog.VI(2).Info("vclock: DoNtpUpdate: end")

	ntpData, err := d.ntpSource.NtpSync(NtpSampleCount)
	if err != nil {
		vlog.Errorf("vclock: DoNtpUpdate: failed to fetch NTP time: %v", err)
		return err
	}
	vlog.VI(2).Infof("vclock: DoNtpUpdate: NTP skew is %v", ntpData.skew)

	err = d.vclock.UpdateVClockData(func(data *VClockData) (*VClockData, error) {
		now, elapsedTime, err := d.vclock.SysClockVals()
		if err != nil {
			vlog.Errorf("vclock: DoNtpUpdate: SysClockVals failed: %v", err)
			return nil, err
		}

		// Only update skew if the delta is greater than NtpSkewDeltaThreshold, to
		// avoid constant tweaking of the clock.
		delta := ntpData.skew - data.Skew
		if abs(delta) > NtpSkewDeltaThreshold {
			vlog.VI(2).Infof("vclock: DoNtpUpdate: NTP time minus Syncbase vclock time is %v; updating Skew", delta)
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
		vlog.Errorf("vclock: DoNtpUpdate: update failed: %v", err)
	}
	return err
}

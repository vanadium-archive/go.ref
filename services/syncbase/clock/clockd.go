// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

// ClockD provides a daemon to run services provided within this package
// like clockservice and ntpservice. These services are run regularly
// in an independent go routine and can be exited by calling Close().
import (
	"sync"
	"time"

	"v.io/v23/context"
	"v.io/x/lib/vlog"
)

const (
	// clockCheckInterval is the duration between two consecutive runs of
	// clockservice to catch any changes to the system clock.
	clockCheckInterval = 1 * time.Second

	// ntpCheckInterval is the duration between two consecutive clock syncs with
	// NTP. It allows us to keep the syncbase clock in sync with NTP.
	ntpCheckInterval = 1 * time.Hour
)

type ClockD struct {
	vclock *VClock

	// State to coordinate shutdown of spawned goroutines.
	pending sync.WaitGroup
	closed  chan struct{}
}

func StartClockD(ctx *context.T, clock *VClock) *ClockD {
	clockD := &ClockD{
		vclock: clock,
	}
	clockD.closed = make(chan struct{})
	clockD.pending.Add(1)

	go clockD.runLoop()

	return clockD
}

func (cd *ClockD) runLoop() {
	vlog.VI(1).Infof("clockd: loop started")
	defer vlog.VI(1).Infof("clockd: loop ended")
	defer cd.pending.Done()

	ticker := time.NewTicker(clockCheckInterval)
	defer ticker.Stop()

	var count int64 = 0
	ntpCheckCount := ntpCheckInterval.Nanoseconds() / clockCheckInterval.Nanoseconds()
	for {
		// Run task first and then wait on ticker. This allows us to run the
		// clockd tasks right away when syncbase starts regardless of the
		// loop interval.
		if count == 0 {
			cd.vclock.runNtpCheck()
		} else {
			cd.vclock.runClockCheck()
		}
		count = (count + 1) % ntpCheckCount
		vlog.VI(5).Infof("clockd: round of clockD run finished")

		select {
		case <-cd.closed:
			vlog.VI(1).Infof("clockd: channel closed, stop work and exit")
			return

		case <-ticker.C:
		}

		// Give priority to close event if both ticker and closed are
		// simultaneously triggered.
		select {
		case <-cd.closed:
			vlog.VI(1).Info("clockd: channel closed, stop work and exit")
			return

		default:
		}
	}
}

// Close cleans up clock state.
// TODO(jlodhia): Hook it up to server shutdown of syncbased.
func (cd *ClockD) Close() {
	close(cd.closed)
	cd.pending.Wait()
}

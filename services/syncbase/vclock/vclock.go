// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vclock

// This file defines the VClock struct and methods.

import (
	"time"

	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

// VClock holds everything needed to provide UTC time estimates.
type VClock struct {
	SysClock SystemClock
	st       store.Store // Syncbase top-level store (not database-specific)
}

// NewVClock creates a new VClock.
// TODO(sadovsky): Add an option to use (or inject) a fake system clock, and
// hook up the DebugUpdateClock RPC to manipulate this fake clock.
func NewVClock(st store.Store) *VClock {
	return &VClock{
		SysClock: newRealSystemClock(),
		st:       st,
	}
}

////////////////////////////////////////////////////////////////////////////////
// Methods for determining the time

// TODO(sadovsky): Cache VClockData in memory, protected by a lock to ensure
// cache consistency. That way, "Now" won't need to return an error, we won't
// need a separate "NowNoLookup" method, and "ApplySkew" can stop taking
// VClockData as input.

// Now returns our estimate of current UTC time based on SystemClock time and
// our estimate of its skew relative to NTP time.
func (c *VClock) Now() (time.Time, error) {
	data := &VClockData{}
	if err := c.GetVClockData(data); err != nil {
		vlog.Errorf("vclock: error fetching VClockData: %v", err)
		return time.Time{}, err
	}
	return c.NowNoLookup(data), nil
}

// NowNoLookup is like Now, but uses the given VClockData instead of fetching
// VClockData from the store.
func (c *VClock) NowNoLookup(data *VClockData) time.Time {
	return c.ApplySkew(c.SysClock.Now(), data)
}

// ApplySkew is like NowNoLookup, but applies skew correction to the given
// SystemClock time instead of using c.SysClock.Now().
func (c *VClock) ApplySkew(sysTime time.Time, data *VClockData) time.Time {
	return sysTime.Add(time.Duration(data.Skew))
}

////////////////////////////////////////////////////////////////////////////////
// Methods for initializing, reading, and updating VClockData in the store

const vclockDataKey = util.VClockPrefix

// GetVClockData fills 'data' with VClockData read from the store.
func (c *VClock) GetVClockData(data *VClockData) error {
	return util.Get(nil, c.st, vclockDataKey, data)
}

// InitVClockData initializes VClockData in the store (if needed).
func (c *VClock) InitVClockData() error {
	return store.RunInTransaction(c.st, func(tx store.Transaction) error {
		if err := util.Get(nil, tx, vclockDataKey, &VClockData{}); err == nil {
			return nil
		} else if verror.ErrorID(err) != verror.ErrNoExist.ID {
			return err
		}
		// No existing VClockData; write fresh VClockData.
		now := c.SysClock.Now()
		elapsedTime, err := c.SysClock.ElapsedTime()
		if err != nil {
			return err
		}
		return util.Put(nil, tx, vclockDataKey, &VClockData{
			SystemTimeAtBoot:     now.Add(-elapsedTime),
			ElapsedTimeSinceBoot: elapsedTime,
		})
	})
}

// UpdateVClockData reads VClockData from the store, applies the given function
// to produce new VClockData, and writes the resulting VClockData back to the
// store. The entire read-modify-write operation is performed inside of a
// transaction.
func (c *VClock) UpdateVClockData(fn func(*VClockData) (*VClockData, error)) error {
	return store.RunInTransactionWithOpts(c.st, &store.TransactionOptions{NumAttempts: 1}, func(tx store.Transaction) error {
		data := &VClockData{}
		if err := util.Get(nil, tx, vclockDataKey, data); err != nil {
			return err
		}
		if newVClockData, err := fn(data); err != nil {
			return err
		} else {
			return util.Put(nil, tx, vclockDataKey, newVClockData)
		}
	})
}

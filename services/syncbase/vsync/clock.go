// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"fmt"
	"time"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/vclock"
)

// GetTime implements the responder side of the GetTime RPC.
// TODO(sadovsky): This method does zero authorization, which means any client
// can send a syncgroup these requests. This seems undesirable.
func (s *syncService) GetTime(ctx *context.T, call rpc.ServerCall, req interfaces.TimeReq, initiator string) (interfaces.TimeResp, error) {
	vlog.VI(2).Infof("sync: GetTime: begin: from initiator %s", initiator)
	defer vlog.VI(2).Infof("sync: GetTime: end: from initiator %s", initiator)

	// TimeResp includes our recvTs and sendTs, derived from system clock values
	// fetched at the beginning and end of RPC handling respectively. As such, we
	// must make sure the system clock isn't changed in the interim.
	recvElapsedTime, err := s.vclock.SysClock.ElapsedTime()
	// Will be adjusted to recvTs once we've read ClockData.
	recvSysTs := s.vclock.SysClock.Now()

	// Check err after fetching recvSysTs to minimize the time gap between
	// sampling recvElapsedTime and recvSysTs.
	if err != nil {
		vlog.Errorf("sync: GetTime: error fetching elapsed time: %v", err)
		return interfaces.TimeResp{}, verror.New(interfaces.ErrGetTimeFailed, ctx, err)
	}

	// Fetch local vclock data.
	data := &vclock.VClockData{}
	if err := s.vclock.GetVClockData(data); err != nil {
		vlog.Errorf("sync: GetTime: error fetching VClockData: %v", err)
		return interfaces.TimeResp{}, verror.New(interfaces.ErrGetTimeFailed, ctx, err)
	}

	// Compute recvTs and sendTs.
	recvTs := s.vclock.ApplySkew(recvSysTs, data)
	sendTs := s.vclock.NowNoLookup(data)

	// Check that our system clock hasn't changed in the interim.
	if err := hasSysClockChanged(s.vclock, recvTs, sendTs, recvElapsedTime); err != nil {
		vlog.Errorf("sync: GetTime: %v", err)
		return interfaces.TimeResp{}, verror.New(interfaces.ErrGetTimeFailed, ctx, err)
	}

	return interfaces.TimeResp{
		OrigTs:     req.SendTs,
		RecvTs:     recvTs,
		SendTs:     sendTs,
		LastNtpTs:  data.LastNtpTs,
		NumReboots: data.NumReboots,
		NumHops:    data.NumHops,
	}, nil
}

// syncVClock syncs the syncbase vclock with peer's syncbase vclock.
func (s *syncService) syncVClock(ctx *context.T, peer connInfo) error {
	vlog.VI(2).Infof("sync: syncVClock: begin: contacting peer %v", peer)
	defer vlog.VI(2).Infof("sync: syncVClock: end: contacting peer %v", peer)

	c := s.vclock
	return c.UpdateVClockData(func(data *vclock.VClockData) (*vclock.VClockData, error) {
		// For detecting system clock changes. See comments in GetTime() for
		// explanation.
		origElapsedTime, err := c.SysClock.ElapsedTime()
		if err != nil {
			vlog.Errorf("sync: syncVClock: error fetching elapsed time: %v", err)
			return nil, err
		}

		// TODO(sadovsky): Propagate the returned connInfo back to syncer so that we
		// leverage what we learned here about available mount tables and endpoints
		// when we continue on to the next initiation phase.
		_, timeRespInt, err := runAtPeer(ctx, peer, func(ctx *context.T, peer string) (interface{}, error) {
			return interfaces.SyncClient(peer).GetTime(ctx, interfaces.TimeReq{SendTs: c.NowNoLookup(data)}, s.name)
		})
		if err != nil {
			vlog.Errorf("sync: syncVClock: GetTime failed: %v", err)
			return nil, err
		}
		timeResp, ok := timeRespInt.(interfaces.TimeResp)
		if !ok {
			err := fmt.Errorf("unexpected GetTime response type: %#v", timeResp)
			vlog.Fatal(err)
			return nil, err
		}

		recvTs := c.NowNoLookup(data)

		// Check that our system clock hasn't changed since reading origElapsedTime.
		if err := hasSysClockChanged(c, timeResp.OrigTs, recvTs, origElapsedTime); err != nil {
			vlog.Errorf("sync: syncVClock: %v", err)
			return nil, err
		}

		// TODO(sadovsky): (Noticed while refactoring code.) Shouldn't we check for
		// hasSysClockChanged after calling MaybeUpdateFromPeerData, given that
		// MaybeUpdateFromPeerData reads from the system clock?
		if data, err := vclock.MaybeUpdateFromPeerData(c.SysClock, data, toPeerSyncData(&timeResp, recvTs)); err != nil {
			if _, ok := err.(*vclock.NoUpdateErr); ok {
				vlog.VI(2).Infof("sync: syncVClock: decided not to update VClockData: %v", err)
			} else {
				vlog.Errorf("sync: syncVClock: error updating VClockData: %v", err)
			}
			return nil, err
		} else {
			return data, nil
		}
	})
}

func hasSysClockChanged(c *vclock.VClock, t1, t2 time.Time, e1 time.Duration) error {
	e2, err := c.SysClock.ElapsedTime()
	if err != nil {
		return fmt.Errorf("error fetching elapsed time: %v", err)
	}
	if vclock.HasSysClockChanged(t1, t2, e1, e2) {
		return fmt.Errorf("sysclock changed during GetTime")
	}
	return nil
}

func toPeerSyncData(resp *interfaces.TimeResp, recvTs time.Time) *vclock.PeerSyncData {
	return &vclock.PeerSyncData{
		MySendTs:   resp.OrigTs,
		RecvTs:     resp.RecvTs,
		SendTs:     resp.SendTs,
		MyRecvTs:   recvTs,
		LastNtpTs:  resp.LastNtpTs,
		NumReboots: resp.NumReboots,
		NumHops:    resp.NumHops,
	}
}

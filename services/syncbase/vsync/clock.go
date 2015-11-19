// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"fmt"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/clock"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
)

// GetTime implements the responder side of the GetTime RPC.
func (s *syncService) GetTime(ctx *context.T, call rpc.ServerCall, req interfaces.TimeReq, initiator string) (interfaces.TimeResp, error) {
	// To make sure that the system clock did not change between fetching
	// receive timestamp and send timestamp, we get the elapsed time since
	// boot (which is immutable) before registering the receive timestamp and
	// after registering the send timestamp and call HasSysClockChanged()
	// to verify if the clock changed in between or not. If it did, we return
	// ErrInternal as response.
	elapsedRecv, err := s.vclock.SysClock.ElapsedTime()

	// In order to get the receive timestamp as close to the reciept of the req
	// we need to first take a timestamp from the system clock and then lookup
	// clock data to convert this timestamp into vclock timestamp to avoid
	// adding the lookup time to receive timestamp.
	sysTs := s.vclock.SysClock.Now()
	if err != nil {
		// We do this test after fetching sysTs to make sure that the sysTs is
		// as close to the receipt time of request as possible.
		vlog.Errorf("sync: GetTime: error while fetching elapsed time: %v", err)
		return interfaces.TimeResp{}, err
	}

	// Lookup local clock data.
	clockData := &clock.ClockData{}
	if err := s.vclock.GetClockData(s.vclock.St(), clockData); err != nil {
		vlog.Errorf("sync: GetTime: error while fetching clock data: %v", err)
		return interfaces.TimeResp{}, err
	}

	// Convert the sysTs that was registered above into vclock ts.
	recvTs := s.vclock.VClockTs(sysTs, *clockData)
	sendTs := s.vclock.NowNoLookup(*clockData)

	// Make sure that the system clock did not change since the request was
	// sent.
	if err := isClockChanged(s.vclock, recvTs, sendTs, elapsedRecv); err != nil {
		vlog.Errorf("sync: GetTime: %v", err)
		return interfaces.TimeResp{}, verror.NewErrInternal(ctx)
	}

	resp := interfaces.TimeResp{
		OrigTs:     req.SendTs,
		RecvTs:     recvTs,
		SendTs:     sendTs,
		LastNtpTs:  clockData.LastNtpTs,
		NumReboots: clockData.NumReboots,
		NumHops:    clockData.NumHops,
	}
	return resp, nil
}

// syncClock syncs the syncbase clock with peer's syncbase clock.
// TODO(jlodhia): Refactor the mount table entry search code based on the
// unified solution for looking up peer once it exists.
func (s *syncService) syncClock(ctx *context.T, peer connInfo) error {
	vlog.VI(2).Infof("sync: syncClock: begin: contacting peer %v", peer)
	defer vlog.VI(2).Infof("sync: syncClock: end: contacting peer %v", peer)

	info := s.copyMemberInfo(ctx, peer.relName)
	if info == nil {
		vlog.Fatalf("sync: syncClock: missing information in member view for %v", peer)
	}

	if len(info.mtTables) < 1 && peer.addrs == nil {
		vlog.Errorf("sync: syncClock: no mount tables or endpoints found to connect to peer %v", peer)
		return verror.New(verror.ErrInternal, ctx, peer.relName, peer.addrs, "no mount tables or endpoints found")
	}

	if peer.addrs != nil {
		vlog.VI(4).Infof("sync: syncClock: trying neighborhood addrs for peer %v", peer)

		for _, addr := range peer.addrs {
			absName := naming.Join(addr, util.SyncbaseSuffix)
			if err := syncWithPeer(ctx, s.vclock, absName, s.name); verror.ErrorID(err) != interfaces.ErrConnFail.ID {
				return err
			}
		}
	} else {
		vlog.VI(4).Infof("sync: syncClock: trying mount tables for peer %v", peer)

		for mt, _ := range info.mtTables {
			absName := naming.Join(mt, peer.relName, util.SyncbaseSuffix)
			if err := syncWithPeer(ctx, s.vclock, absName, s.name); verror.ErrorID(err) != interfaces.ErrConnFail.ID {
				return err
			}
		}
	}

	vlog.Errorf("sync: syncClock: couldn't connect to peer %v", peer)
	return verror.New(interfaces.ErrConnFail, ctx, peer.relName, peer.addrs, "all mount tables and endpoints failed")
}

// syncWithPeer tries to sync local clock with peer syncbase clock.
// Returns error if the GetTime() rpc returns error.
func syncWithPeer(ctx *context.T, vclock *clock.VClock, absPeerName string, myName string) error {
	c := interfaces.SyncClient(absPeerName)

	// Since syncing time with peer is a non critical task, its not necessary
	// to retry it if the transaction fails. Peer time sync will be run
	// frequently and so the next run will be the next retry.
	tx := vclock.St().NewTransaction()

	// Lookup clockData. If error received return ErrBadState to let the caller
	// distinguish it from a network error so that it can stop looping through
	// other mounttable entires.
	localData := &clock.ClockData{}
	err := vclock.GetClockData(tx, localData)
	if err != nil {
		// To avoid dealing with non existent clockdata during clock sync
		// just abort clock sync and wait for the clockservice to create
		// clockdata.
		vlog.Errorf("sync: syncClock: error while fetching local clock data: %v", err)
		tx.Abort()
		return nil
	}

	// See comments in GetTime() related to catching system clock changing
	// midway.
	elapsedOrig, err := vclock.SysClock.ElapsedTime()
	if err != nil {
		vlog.Errorf("sync: syncClock: error while fetching elapsed time: %v", err)
		return nil
	}

	tctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// We start a timer to bound the amount of time we wait to
	// initiate a connection.
	t := time.AfterFunc(connectionTimeOut, cancel)

	timeResp, reqErr := c.GetTime(tctx, makeTimeReq(vclock, localData), myName)
	t.Stop()

	if reqErr == nil {
		recvTs := vclock.NowNoLookup(*localData)

		// Make sure that the system clock did not change since the request was
		// sent.
		if err := isClockChanged(vclock, timeResp.OrigTs, recvTs, elapsedOrig); err != nil {
			vlog.Errorf("sync: syncClock: %v", err)
			return nil
		}

		vlog.VI(4).Infof("sync: syncClock: connection established on %s", absPeerName)
		updated := vclock.ProcessPeerClockData(tx, toPeerSyncData(&timeResp, recvTs), localData)
		if updated {
			if commitErr := tx.Commit(); commitErr != nil {
				vlog.VI(2).Infof("sync: syncClock: error while commiting tx: %v", commitErr)
			}
		} else {
			tx.Abort()
		}
	} else if (verror.ErrorID(reqErr) == verror.ErrNoExist.ID) || (verror.ErrorID(reqErr) == verror.ErrInternal.ID) {
		vlog.Errorf("sync: syncClock: error returned by peer %s: %v", absPeerName, err)
	} else {
		vlog.Errorf("sync: syncClock: received network error: %v", reqErr)
		reqErr = verror.New(interfaces.ErrConnFail, ctx, absPeerName)
	}
	// Return error received while making request if any to the caller.
	return reqErr
}

func isClockChanged(vclock *clock.VClock, t1, t2 time.Time, e1 time.Duration) error {
	e2, err := vclock.SysClock.ElapsedTime()
	if err != nil {
		return fmt.Errorf("error while fetching elapsed time: %v", err)
	}
	if clock.HasSysClockChanged(t1, t2, e1, e2) {
		return fmt.Errorf("system clock changed midway through sycning clock with peer.")
	}
	return nil
}

func makeTimeReq(vclock *clock.VClock, clockData *clock.ClockData) interfaces.TimeReq {
	return interfaces.TimeReq{
		SendTs: vclock.NowNoLookup(*clockData),
	}
}

func toPeerSyncData(resp *interfaces.TimeResp, recvTs time.Time) *clock.PeerSyncData {
	return &clock.PeerSyncData{
		MySendTs:   resp.OrigTs,
		RecvTs:     resp.RecvTs,
		SendTs:     resp.SendTs,
		MyRecvTs:   recvTs,
		LastNtpTs:  resp.LastNtpTs,
		NumReboots: resp.NumReboots,
		NumHops:    resp.NumHops,
	}
}

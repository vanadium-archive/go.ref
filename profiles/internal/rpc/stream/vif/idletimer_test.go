// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"testing"
	"time"

	"v.io/x/ref/profiles/internal/rpc/stream/id"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
)

func TestIdleTimer(t *testing.T) {
	const (
		idleTime = 5 * time.Millisecond
		waitTime = idleTime * 2

		vc1 id.VC = 1
		vc2 id.VC = 2

		flow1        id.Flow = vc.NumReservedFlows
		flow2        id.Flow = vc.NumReservedFlows + 1
		flowReserved id.Flow = vc.NumReservedFlows - 1
	)

	notify := make(chan interface{})
	notifyFunc := func(vci id.VC) { notify <- vci }

	m := newIdleTimerMap(notifyFunc)

	// An empty map. Should not be notified.
	if err := WaitForNotifications(notify, waitTime); err != nil {
		t.Error(err)
	}

	m.Insert(vc1, idleTime)

	// A new empty VC. Should be notified.
	if err := WaitForNotifications(notify, waitTime, vc1); err != nil {
		t.Error(err)
	}

	m.Delete(vc1)
	m.Insert(vc1, idleTime)

	// A VC with one flow. Should not be notified.
	m.InsertFlow(vc1, flow1)
	if err := WaitForNotifications(notify, waitTime); err != nil {
		t.Error(err)
	}

	// Try to delete non-existent flow. Should not be notified.
	m.DeleteFlow(vc1, flow2)
	if err := WaitForNotifications(notify, waitTime); err != nil {
		t.Error(err)
	}

	// Delete the flow. Should be notified.
	m.DeleteFlow(vc1, flow1)
	if err := WaitForNotifications(notify, waitTime, vc1); err != nil {
		t.Error(err)
	}

	// Try to delete the deleted flow again. Should not be notified.
	m.DeleteFlow(vc1, flow1)
	if err := WaitForNotifications(notify, waitTime); err != nil {
		t.Error(err)
	}

	m.Delete(vc1)
	m.Insert(vc1, idleTime)

	// Delete an empty VC. Should not be notified.
	m.Delete(vc1)
	if err := WaitForNotifications(notify, waitTime); err != nil {
		t.Error(err)
	}

	m.Insert(vc1, idleTime)

	// A VC with two flows.
	m.InsertFlow(vc1, flow1)
	m.InsertFlow(vc1, flow2)

	// Delete the first flow twice. Should not be notified.
	m.DeleteFlow(vc1, flow1)
	m.DeleteFlow(vc1, flow1)
	if err := WaitForNotifications(notify, waitTime); err != nil {
		t.Error(err)
	}

	// Delete the second flow. Should be notified.
	m.DeleteFlow(vc1, flow2)
	if err := WaitForNotifications(notify, waitTime, vc1); err != nil {
		t.Error(err)
	}

	m.Delete(vc1)
	m.Insert(vc1, idleTime)

	// Insert a reserved flow. Should be notified.
	m.InsertFlow(vc1, flowReserved)
	if err := WaitForNotifications(notify, waitTime, vc1); err != nil {
		t.Error(err)
	}

	m.Delete(vc1)
	m.Insert(vc1, idleTime)
	m.Insert(vc2, idleTime)

	// Multiple VCs. Should be notified for each.
	if err := WaitForNotifications(notify, waitTime, vc1, vc2); err != nil {
		t.Error(err)
	}

	m.Delete(vc1)
	m.Delete(vc2)
	m.Insert(vc1, idleTime)

	// Stop the timer. Should not be notified.
	m.Stop()
	if m.Insert(vc1, idleTime) {
		t.Fatal("timer has been stopped, but can insert a vc")
	}
	if err := WaitForNotifications(notify, waitTime); err != nil {
		t.Error(err)
	}
}

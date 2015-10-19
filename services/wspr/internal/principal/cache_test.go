// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package principal

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"reflect"
	"testing"
	"time"

	"v.io/v23/security"
)

// manualTrigger provides a gc trigger that can be signaled manually
type manualTrigger struct {
	gcHasRun bool
	ch       chan time.Time
	nextCh   chan bool
}

func newManualTrigger() *manualTrigger {
	return &manualTrigger{
		ch:     make(chan time.Time),
		nextCh: make(chan bool),
	}
}

// manualTrigger is the trigger that should be provided in GC policy config.
// It returns a chan time.Time that resolves immediately after next is called.
func (mt *manualTrigger) waitForNextGc() <-chan time.Time {
	if !mt.gcHasRun {
		mt.gcHasRun = true
	} else {
		mt.nextCh <- true
	}
	return mt.ch
}

// next should be called to trigger the next policy trigger event.
func (mt *manualTrigger) next() {
	mt.ch <- time.Time{}
	<-mt.nextCh
}

// Test just to confirm it signals in order as expected.
func TestManualTrigger(t *testing.T) {
	mt := newManualTrigger()

	countTriggers := 0
	go func() {
		for i := 0; i < 100; i++ {
			<-mt.waitForNextGc()
			countTriggers++
		}
	}()

	for i := 1; i <= 99; i++ {
		mt.next()
		if countTriggers != i {
			t.Errorf("Expected %d triggers, got %d", i, countTriggers)
		}
	}
}

func newSigner() security.Signer {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return security.NewInMemoryECDSASigner(key)
}

func TestBlessingsCache(t *testing.T) {
	notificationCh := make(chan []BlessingsCacheMessage, 1)
	notifier := func(msg []BlessingsCacheMessage) {
		notificationCh <- msg
	}

	mt := newManualTrigger()

	// Create a BlessingsCache with a GC policy that we can trigger on demand.
	onDemandGCPolicy := &BlessingsCacheGCPolicy{
		nextTrigger: mt.waitForNextGc,
	}
	bc := NewBlessingsCache(notifier, onDemandGCPolicy)

	// Blessings for the tests.
	p, err := security.CreatePrincipal(newSigner(), nil, nil)
	if err != nil {
		t.Fatal("Failed to create principal: ", err)
	}
	blessA, err := p.BlessSelf("A")
	if err != nil {
		t.Fatal("Failed to bless A: ", err)
	}
	blessB, err := p.BlessSelf("B")
	if err != nil {
		t.Fatal("Failed to bless B: ", err)
	}
	blessC, err := p.BlessSelf("C")
	if err != nil {
		t.Fatal("Failed to bless C: ", err)
	}

	// First do puts and make sure the ids are reasonable.
	idA := bc.Put(blessA)
	expectAddMessage(t, notificationCh, 1, blessA)
	idB := bc.Put(blessB)
	expectAddMessage(t, notificationCh, 2, blessB)
	if idA == idB {
		t.Errorf("A and B unexpectedly had same id: %v", idA)
	}

	idA2 := bc.Put(blessA)
	expectNoMessage(t, notificationCh)
	if idA2 != idA {
		t.Errorf("A and A2 expected to have same id, but they were %v and %v",
			idA, idA2)
	}

	// Now perform GC. Check that the values are still in the cache.
	mt.next()
	expectNoMessage(t, notificationCh)

	idGc1A := bc.Put(blessA)
	idGc1B := bc.Put(blessB)
	expectNoMessage(t, notificationCh)
	if idA != idGc1A {
		t.Errorf("Expected to get same id after one gc of A, but got %v and %v",
			idA, idGc1A)
	}
	if idB != idGc1B {
		t.Errorf("Expected to get same id after one gc of B, but got %v and %v",
			idB, idGc1B)
	}

	// Now perform GC to clear the dirty bits.
	mt.next()
	expectNoMessage(t, notificationCh)

	// Update B and add C.
	idGc2B := bc.Put(blessB)
	expectNoMessage(t, notificationCh)
	if idB != idGc2B {
		t.Errorf("Expected to get same id after two gcs of B, but got %v and %v",
			idB, idGc2B)
	}
	idC := bc.Put(blessC)
	expectAddMessage(t, notificationCh, 3, blessC)
	if idC == idA || idC == idB {
		t.Error("C was unexpectedly the same as A or B")
	}

	// Perform GC. A should be removed but B and C should stay.
	mt.next()
	expectDeleteMessage(t, notificationCh, BlessingsCacheDeleteMessage{CacheId: 1, DeleteAfter: 3})
	if idB != bc.Put(blessB) {
		t.Errorf("B seems to have been cleaned up as it was given a new id")
	}
	if idC != bc.Put(blessC) {
		t.Errorf("C seems to have been cleaned up as it was given a new id")
	}
	expectNoMessage(t, notificationCh)

	// Perform GC twice to remove the other items.
	mt.next()
	expectNoMessage(t, notificationCh)
	mt.next()
	expectDeleteMessage(t, notificationCh,
		BlessingsCacheDeleteMessage{CacheId: 2, DeleteAfter: 4},
		BlessingsCacheDeleteMessage{CacheId: 3, DeleteAfter: 2})

	// No notifications should occur on further GCs.
	mt.next()
	mt.next()
	mt.next()
	// Note that this should only be reached after the second GC is done because
	// the GC trigger channel has size 1.
	expectNoMessage(t, notificationCh)

	bc.Stop()
}

func expectNoMessage(t *testing.T, notificationCh chan []BlessingsCacheMessage) {
	select {
	case <-notificationCh:
		t.Errorf("Got message when none expected")
	default:
	}
}

func expectAddMessage(t *testing.T, notificationCh chan []BlessingsCacheMessage, id BlessingsId, bless security.Blessings) {
	select {
	case notifications := <-notificationCh:
		if len(notifications) != 1 {
			t.Fatalf("Got invalid add message with %d messages", len(notifications))
		}
		addMsg := notifications[0].(BlessingsCacheMessageAdd).Value
		if got, want := addMsg.CacheId, id; got != want {
			t.Errorf("Unexpected id in add message: %v. Wanted: %v", got, want)
		}
		if got, want := addMsg.Blessings, bless; !reflect.DeepEqual(got, want) {
			t.Errorf("Blessings unexpectedly not equal. Got %v, want %v", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Timed out waiting for notification")
	}
}

func expectDeleteMessage(t *testing.T, notificationCh chan []BlessingsCacheMessage, expected ...BlessingsCacheDeleteMessage) {
	select {
	case notifications := <-notificationCh:
		if len(notifications) != len(expected) {
			t.Fatalf("Got %d notifications but expected %d delete notifications", len(notifications), len(expected))
		}
		for _, notification := range notifications {
			delNotif := notification.(BlessingsCacheMessageDelete).Value
			var foundMatch bool
			for _, expectedNotif := range expected {
				if reflect.DeepEqual(delNotif, expectedNotif) {
					foundMatch = true
				}
			}
			if !foundMatch {
				t.Errorf("Unexpected delete notification: %v", delNotif)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Timed out waiting for notification")
	}
}

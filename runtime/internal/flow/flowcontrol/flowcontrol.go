// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flowcontrol

import (
	"bytes"
	"fmt"
	"sync"

	"v.io/v23/context"
)

// Runners are called by Workers.  For a given flow controller
// only one Runner will be running at a time.  tokens specifies
// the number of tokens available for this call.  Implementors
// should return the number of tokens used, whether they are done
// with all their work, and any error encountered.
// Runners will be called repeatedly within a single Run call until
// either err != nil or done is true.
type Runner func(tokens int) (used int, done bool, err error)

type counterState struct {
	// TODO(mattr): Add deficit if we allow multi-slice writes.
	borrowed     int  // Number of tokens borrowed from the shared pool.
	released     int  // Number of tokens available via our flow control counters.
	everReleased bool // True if tokens have ever been released to this worker.
}

type state int

const (
	idle = state(iota)
	running
	active
)

// Worker represents a single flowcontrolled worker.
// Workers keep track of flow control counters to ensure
// producers do not overwhelm consumers.  Only one Worker
// will be executing at a time.
type Worker struct {
	fc       *FlowController
	priority int
	work     chan struct{}

	// These variables are protected by fc.mu.
	counters   *counterState // State related to the flow control counters.
	state      state
	next, prev *Worker // Used as a list when in an active queue.
}

// Run runs r potentially multiple times.
// Only one worker's r function will run at a time for a given FlowController.
// A single worker's Run function should not be called concurrently from multiple
// goroutines.
func (w *Worker) Run(ctx *context.T, r Runner) (err error) {
	w.fc.mu.Lock()
	if w.state != idle {
		w.fc.mu.Unlock()
		return NewErrConcurrentRun(ctx)
	}

	w.state = running
	if w.readyLocked() {
		w.fc.activateLocked(w)
		w.state = active
	}

	for {
		next := w.fc.nextWorkerLocked()
		for w.fc.writing != w && err == nil {
			w.fc.mu.Unlock()
			if next != nil {
				next.work <- struct{}{}
			}
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case <-w.work:
			}
			w.fc.mu.Lock()
		}
		if err != nil {
			break
		}

		toWrite := w.fc.mtu
		if w.counters != nil {
			if !w.counters.everReleased {
				toWrite = min(w.fc.shared, w.fc.mtu)
				w.counters.released += toWrite
				w.counters.borrowed += toWrite
				w.fc.shared -= toWrite
			} else {
				toWrite = min(w.counters.released, w.fc.mtu)
			}
		}

		w.fc.mu.Unlock()
		var written int
		var done bool
		written, done, err = r(toWrite)
		w.fc.mu.Lock()

		if w.counters != nil {
			w.counters.released -= written
			if w.counters.released > 0 && w.counters.borrowed > 0 {
				toReturn := min(w.counters.released, w.counters.borrowed)
				w.counters.borrowed -= toReturn
				w.counters.released -= toReturn
				w.fc.shared += toReturn
			}
		}

		w.fc.writing = nil
		if err != nil || done {
			break
		}
		if !w.readyLocked() {
			w.fc.deactivateLocked(w)
			w.state = running
		}
	}

	w.state = idle
	w.fc.deactivateLocked(w)
	next := w.fc.nextWorkerLocked()
	w.fc.mu.Unlock()
	if next != nil {
		next.work <- struct{}{}
	}
	return err
}

func (w *Worker) releaseLocked(ctx *context.T, tokens int) {
	if w.counters == nil {
		return
	}
	w.counters.everReleased = true
	if w.counters.borrowed > 0 {
		n := min(w.counters.borrowed, tokens)
		w.counters.borrowed -= n
		w.fc.shared += n
		tokens -= n
	}
	w.counters.released += tokens
	if w.state == running && w.readyLocked() {
		w.fc.activateLocked(w)
	}
}

// Release releases tokens to this worker.
// Workers will first repay any debts to the flow controllers shared pool
// and use any surplus in subsequent calls to Run.
func (w *Worker) Release(ctx *context.T, tokens int) {
	w.fc.mu.Lock()
	w.releaseLocked(ctx, tokens)
	next := w.fc.nextWorkerLocked()
	w.fc.mu.Unlock()
	if next != nil {
		next.work <- struct{}{}
	}
}

func (w *Worker) readyLocked() bool {
	if w.counters == nil {
		return true
	}
	return w.counters.released > 0 || (!w.counters.everReleased && w.fc.shared > 0)
}

// FlowController manages multiple Workers to ensure only one runs at a time.
// The workers also obey counters so that producers don't overwhelm consumers.
type FlowController struct {
	mtu int

	mu      sync.Mutex
	shared  int
	active  []*Worker
	writing *Worker
}

// New creates a new FlowController.  Shared is the number of shared tokens
// that flows can borrow from before they receive their first Release.
// Mtu is the maximum number of tokens to be consumed by a single Runner
// invocation.
func New(shared, mtu int) *FlowController {
	return &FlowController{shared: shared, mtu: mtu}
}

// NewWorker creates a new worker.  Workers keep track of token counters
// for a flow controlled process.  The order that workers
// execute is controlled by priority.  Higher priority
// workers that are ready will run before any lower priority
// workers.
func (fc *FlowController) NewWorker(priority int) *Worker {
	w := &Worker{
		fc:       fc,
		priority: priority,
		work:     make(chan struct{}),
		counters: &counterState{},
	}
	w.next, w.prev = w, w
	return w
}

type Release struct {
	Worker *Worker
	Tokens int
}

// Release releases to many Workers atomically.  It is conceptually
// the same as calling release on each worker indepedently.
func (fc *FlowController) Release(ctx *context.T, to []Release) error {
	fc.mu.Lock()
	for _, t := range to {
		if t.Worker.fc != fc {
			return NewErrWrongFlowController(ctx)
		}
		t.Worker.releaseLocked(ctx, t.Tokens)
	}
	next := fc.nextWorkerLocked()
	fc.mu.Unlock()
	if next != nil {
		next.work <- struct{}{}
	}
	return nil
}

// Run runs the given runner on a non-flow controlled Worker.  This
// worker does not wait for any flow control tokens and is limited
// only by the MTU.
func (fc *FlowController) Run(ctx *context.T, p int, r Runner) error {
	w := &Worker{
		fc:       fc,
		priority: p,
		work:     make(chan struct{}),
	}
	w.next, w.prev = w, w
	return w.Run(ctx, r)
}

func (fc *FlowController) nextWorkerLocked() *Worker {
	if fc.writing == nil {
		for p, head := range fc.active {
			if head != nil {
				fc.active[p] = head.next
				fc.writing = head
				return head
			}
		}
	}
	return nil
}

func (fc *FlowController) activateLocked(w *Worker) {
	if w.priority >= len(fc.active) {
		newActive := make([]*Worker, int(w.priority)+1)
		copy(newActive, fc.active)
		fc.active = newActive
	}
	head := fc.active[w.priority]
	if head == nil {
		fc.active[w.priority] = w
	} else {
		w.prev, w.next = head.prev, head
		w.prev.next, w.next.prev = w, w
	}
}

func (fc *FlowController) deactivateLocked(w *Worker) {
	if head := fc.active[w.priority]; head == w {
		if w.next == w {
			fc.active[w.priority] = nil
		} else {
			fc.active[w.priority] = w.next
		}
	}
	w.next.prev, w.prev.next = w.prev, w.next
	w.next, w.prev = w, w
}

func (fc *FlowController) numActive() int {
	n := 0
	fc.mu.Lock()
	for _, head := range fc.active {
		if head != nil {
			n++
			for cur := head.next; cur != head; cur = cur.next {
				n++
			}
		}
	}
	fc.mu.Unlock()
	return n
}

// String writes a string representation of the flow controller.
// This can be helpful in debugging.
func (fc *FlowController) String() string {
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "FlowController %p: \n", fc)

	fc.mu.Lock()
	fmt.Fprintf(buf, "writing: %p\n", fc.writing)
	fmt.Fprintln(buf, "active:")
	for p, head := range fc.active {
		fmt.Fprintf(buf, "  %v: %p", p, head)
		if head != nil {
			for cur := head.next; cur != head; cur = cur.next {
				fmt.Fprintf(buf, " %p", cur)
			}
		}
		fmt.Fprintln(buf, "")
	}
	fc.mu.Unlock()
	return buf.String()
}

func min(head int, rest ...int) int {
	for _, r := range rest {
		if r < head {
			head = r
		}
	}
	return head
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"v.io/v23/services/application"
)

// RestartPolicy instances provide a policy for deciding if an
// application should be restarted on failure.
type restartPolicy interface {
	// decide determines if this application instance should be (re)started, returning
	// true if the application should be be (re)started.
	decide(envelope *application.Envelope, instance *instanceInfo) bool
}

// startStub implements a stub RestartPolicy.
type startStub struct {
	start bool
}

// alwaysStart returns a RestartPolicy that always (re)starts the application.
func alwaysStart() restartPolicy {
	return startStub{true}
}

// neverStart returns a RestartPolicy that never (re)starts the application.
func neverStart() restartPolicy {
	return startStub{false}
}

func (s startStub) decide(envelope *application.Envelope, instance *instanceInfo) bool {
	return s.start
}

// Package appcycle is a stripped-down server stub implementation for the
// AppCycle service.  We can't use the generated stub under
// veyron2/services/mgmt/appcycle because that would introduce a recursive
// dependency on veyron/runtimes/google (via veyron2/rt).
//
// TODO(caprita): It would be nice to still use the stub if possible.  Look
// into the feasibility of a generated stub that does not depend on veyron2/rt.
package appcycle

import (
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
)

// AppCycleServerMethods is the interface a server writer
// implements for AppCycle.
//
// AppCycle interfaces with the process running a veyron runtime.
type AppCycleServerMethods interface {
	// Stop initiates shutdown of the server.  It streams back periodic
	// updates to give the client an idea of how the shutdown is
	// progressing.
	Stop(AppCycleStopContext) error
	// ForceStop tells the server to shut down right away.  It can be issued
	// while a Stop is outstanding if for example the client does not want
	// to wait any longer.
	ForceStop(ipc.ServerContext) error
}

// AppCycleServer returns a server stub for AppCycle.
// It converts an implementation of AppCycleServerMethods into
// an object that may be used by ipc.Server.
func AppCycleServer(impl AppCycleServerMethods) AppCycleServerStub {
	return AppCycleServerStub{impl}
}

type AppCycleServerStub struct {
	impl AppCycleServerMethods
}

func (s AppCycleServerStub) Stop(ctx *AppCycleStopContextStub) error {
	return s.impl.Stop(ctx)
}

func (s AppCycleServerStub) ForceStop(call ipc.ServerCall) error {
	return s.impl.ForceStop(call)
}

// AppCycleStopContext represents the context passed to AppCycle.Stop.
type AppCycleStopContext interface {
	ipc.ServerContext
	// SendStream returns the send side of the server stream.
	SendStream() interface {
		// Send places the item onto the output stream.  Returns errors encountered
		// while sending.  Blocks if there is no buffer space; will unblock when
		// buffer space is available.
		Send(item veyron2.Task) error
	}
}

type AppCycleStopContextStub struct {
	ipc.ServerCall
}

func (s *AppCycleStopContextStub) Init(call ipc.ServerCall) {
	s.ServerCall = call
}

func (s *AppCycleStopContextStub) SendStream() interface {
	Send(item veyron2.Task) error
} {
	return implAppCycleStopContextSend{s}
}

type implAppCycleStopContextSend struct {
	s *AppCycleStopContextStub
}

func (s implAppCycleStopContextSend) Send(item veyron2.Task) error {
	return s.s.Send(item)
}

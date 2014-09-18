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

// AppCycleService is the interface the server implements.
type AppCycleService interface {

	// Stop initiates shutdown of the server.  It streams back periodic updates
	// to give the client an idea of how the shutdown is progressing.
	Stop(context ipc.ServerContext, stream AppCycleServiceStopStream) (err error)
	// ForceStop tells the server to shut down right away.  It can be issued while
	// a Stop is outstanding if for example the client does not want to wait any
	// longer.
	ForceStop(context ipc.ServerContext) (err error)
}

// NewServerAppCycle creates a new receiver from the given AppCycleService.
func NewServerAppCycle(server AppCycleService) interface{} {
	return &ServerStubAppCycle{
		service: server,
	}
}

type AppCycleServiceStopStream interface {
	// Send places the item onto the output stream, blocking if there is no buffer
	// space available.
	Send(item veyron2.Task) error
}

// Implementation of the AppCycleServiceStopStream interface that is not exported.
type implAppCycleServiceStopStream struct {
	serverCall ipc.ServerCall
}

func (s *implAppCycleServiceStopStream) Send(item veyron2.Task) error {
	return s.serverCall.Send(item)
}

// ServerStubAppCycle wraps a server that implements
// AppCycleService and provides an object that satisfies
// the requirements of veyron2/ipc.ReflectInvoker.
type ServerStubAppCycle struct {
	service AppCycleService
}

func (s *ServerStubAppCycle) Stop(call ipc.ServerCall) error {
	stream := &implAppCycleServiceStopStream{serverCall: call}
	return s.service.Stop(call, stream)
}

func (s *ServerStubAppCycle) ForceStop(call ipc.ServerCall) error {
	return s.service.ForceStop(call)
}

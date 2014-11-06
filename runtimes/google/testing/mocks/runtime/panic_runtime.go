package runtime

import (
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/config"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vtrace"
)

// PanicRuntime is a dummy implementation of veyron2.Runtime that panics on every
// operation.  This is useful when you want to pass around a non-nil runtime
// implementation but you don't want it to be used.
type PanicRuntime struct {
	unique int // Make non-empty to ensure pointer instances are unique.
}

const badRuntime = "The runtime implmentation should not call methods on runtime intances."

func (*PanicRuntime) Profile() veyron2.Profile                               { panic(badRuntime) }
func (*PanicRuntime) Publisher() *config.Publisher                           { panic(badRuntime) }
func (*PanicRuntime) Principal() security.Principal                          { panic(badRuntime) }
func (*PanicRuntime) NewClient(opts ...ipc.ClientOpt) (ipc.Client, error)    { panic(badRuntime) }
func (*PanicRuntime) NewServer(opts ...ipc.ServerOpt) (ipc.Server, error)    { panic(badRuntime) }
func (*PanicRuntime) Client() ipc.Client                                     { panic(badRuntime) }
func (*PanicRuntime) NewContext() context.T                                  { panic(badRuntime) }
func (*PanicRuntime) WithNewSpan(context.T, string) (context.T, vtrace.Span) { panic(badRuntime) }
func (*PanicRuntime) SpanFromContext(context.T) vtrace.Span                  { panic(badRuntime) }
func (*PanicRuntime) NewStreamManager(opts ...stream.ManagerOpt) (stream.Manager, error) {
	panic(badRuntime)
}
func (*PanicRuntime) NewEndpoint(ep string) (naming.Endpoint, error) { panic(badRuntime) }
func (*PanicRuntime) Namespace() naming.Namespace                    { panic(badRuntime) }
func (*PanicRuntime) Logger() vlog.Logger                            { panic(badRuntime) }
func (*PanicRuntime) NewLogger(name string, opts ...vlog.LoggingOpts) (vlog.Logger, error) {
	panic(badRuntime)
}
func (*PanicRuntime) Stop()                         { panic(badRuntime) }
func (*PanicRuntime) ForceStop()                    { panic(badRuntime) }
func (*PanicRuntime) WaitForStop(chan<- string)     { panic(badRuntime) }
func (*PanicRuntime) AdvanceGoal(delta int)         { panic(badRuntime) }
func (*PanicRuntime) AdvanceProgress(delta int)     { panic(badRuntime) }
func (*PanicRuntime) TrackTask(chan<- veyron2.Task) { panic(badRuntime) }
func (*PanicRuntime) ConfigureReservedName(ipc.Dispatcher, ...ipc.ServerOpt) {
	panic(badRuntime)
}
func (*PanicRuntime) Cleanup() { panic(badRuntime) }

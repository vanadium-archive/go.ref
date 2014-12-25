package runtime

import (
	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/config"
	"v.io/veyron/veyron2/context"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/ipc/stream"
	"v.io/veyron/veyron2/naming"
	"v.io/veyron/veyron2/security"
	"v.io/veyron/veyron2/uniqueid"
	"v.io/veyron/veyron2/vlog"
	"v.io/veyron/veyron2/vtrace"
)

// PanicRuntime is a dummy implementation of veyron2.Runtime that panics on every
// operation.  This is useful when you want to pass around a non-nil runtime
// implementation but you don't want it to be used.
type PanicRuntime struct {
	unique int // Make non-empty to ensure pointer instances are unique.

}

const badRuntime = "The runtime implmentation should not call methods on runtime intances."

func (*PanicRuntime) Profile() veyron2.Profile                            { panic(badRuntime) }
func (*PanicRuntime) AppCycle() veyron2.AppCycle                          { panic(badRuntime) }
func (*PanicRuntime) Publisher() *config.Publisher                        { panic(badRuntime) }
func (*PanicRuntime) Principal() security.Principal                       { panic(badRuntime) }
func (*PanicRuntime) NewClient(opts ...ipc.ClientOpt) (ipc.Client, error) { panic(badRuntime) }
func (*PanicRuntime) NewServer(opts ...ipc.ServerOpt) (ipc.Server, error) { panic(badRuntime) }
func (*PanicRuntime) Client() ipc.Client                                  { panic(badRuntime) }
func (*PanicRuntime) NewContext() context.T                               { panic(badRuntime) }

func (PanicRuntime) WithNewSpan(c context.T, m string) (context.T, vtrace.Span) { return c, &span{m} }

func (*PanicRuntime) SpanFromContext(context.T) vtrace.Span { return &span{} }
func (*PanicRuntime) NewStreamManager(opts ...stream.ManagerOpt) (stream.Manager, error) {
	panic(badRuntime)
}
func (*PanicRuntime) NewEndpoint(ep string) (naming.Endpoint, error) { panic(badRuntime) }
func (*PanicRuntime) Namespace() naming.Namespace                    { panic(badRuntime) }
func (*PanicRuntime) Logger() vlog.Logger                            { panic(badRuntime) }
func (*PanicRuntime) NewLogger(name string, opts ...vlog.LoggingOpts) (vlog.Logger, error) {
	panic(badRuntime)
}
func (*PanicRuntime) ConfigureReservedName(ipc.Dispatcher, ...ipc.ServerOpt) {
	panic(badRuntime)
}
func (*PanicRuntime) VtraceStore() vtrace.Store { panic(badRuntime) }
func (*PanicRuntime) Cleanup()                  { panic(badRuntime) }

type span struct{ m string }

func (s *span) Name() string                   { return s.m + ".panic" }
func (*span) ID() uniqueid.ID                  { return uniqueid.ID{} }
func (s *span) Parent() uniqueid.ID            { return s.ID() }
func (*span) Annotate(string)                  {}
func (*span) Annotatef(string, ...interface{}) {}
func (*span) Finish()                          {}
func (*span) Trace() vtrace.Trace              { return nil }

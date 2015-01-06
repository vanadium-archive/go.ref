package rt

import (
	"fmt"
	"time"

	_ "v.io/core/veyron/lib/stats/sysstats"
	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"

	//iipc "v.io/core/veyron/runtimes/google/ipc"
	iipc "v.io/core/veyron/runtimes/google/ipc"
	imanager "v.io/core/veyron/runtimes/google/ipc/stream/manager"
	"v.io/core/veyron/runtimes/google/ipc/stream/vc"
	inaming "v.io/core/veyron/runtimes/google/naming"
	"v.io/core/veyron/runtimes/google/naming/namespace"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"
)

type contextKey int

const (
	streamManagerKey = contextKey(iota)
	clientKey
	namespaceKey
	loggerKey
	principalKey
	vtraceKey
	reservedNameKey
	protocolsKey
)

func init() {
	veyron2.RegisterRuntime("google", &RuntimeX{})
}

// initRuntimeXContext provides compatibility between Runtime and RuntimeX.
// It is only used during the transition between runtime and
// RuntimeX.  It populates a context with all the subparts that the
// new interface expects to be present.  In the future this work will
// be replaced by RuntimeX.Init()
// TODO(mattr): Remove this after the runtime->runtimex transistion.
func (rt *vrt) initRuntimeXContext(ctx *context.T) *context.T {
	ctx = context.WithValue(ctx, streamManagerKey, rt.sm[0])
	ctx = context.WithValue(ctx, clientKey, rt.client)
	ctx = context.WithValue(ctx, namespaceKey, rt.ns)
	ctx = context.WithValue(ctx, loggerKey, vlog.Log)
	ctx = context.WithValue(ctx, principalKey, rt.principal)
	ctx = context.WithValue(ctx, vtraceKey, rt.traceStore)
	return ctx
}

// RuntimeX implements the veyron2.RuntimeX interface.  It is stateless.
// Please see the interface definition for documentation of the
// individiual methods.
type RuntimeX struct{}

// TODO(mattr): This function isn't used yet.  We'll implement it later
// in the transition.
func (*RuntimeX) Init(ctx *context.T) (*context.T, context.CancelFunc) {
	// TODO(mattr): Here we need to do a bunch of one time init, like parsing flags
	// and reading the credentials, init logging and verror, start an appcycle manager.
	// TODO(mattr): Here we need to arrange for a long of one time cleanup
	// when cancel is called. Dump vtrace, shotdown signalhandling, shutdownlogging,
	// shutdown the appcyclemanager.
	return nil, nil
}

func (*RuntimeX) NewEndpoint(ep string) (naming.Endpoint, error) {
	return inaming.NewEndpoint(ep)
}

func (r *RuntimeX) NewServer(ctx *context.T, opts ...ipc.ServerOpt) (ipc.Server, error) {
	// Create a new RoutingID (and StreamManager) for each server.
	_, sm, err := r.SetNewStreamManager(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create ipc/stream/Manager: %v", err)
	}

	ns, _ := ctx.Value(namespaceKey).(naming.Namespace)
	principal, _ := ctx.Value(principalKey).(security.Principal)

	otherOpts := append([]ipc.ServerOpt{}, opts...)
	otherOpts = append(otherOpts, vc.LocalPrincipal{principal})
	if reserved, ok := ctx.Value(reservedNameKey).(*reservedNameDispatcher); ok {
		otherOpts = append(otherOpts, options.ReservedNameDispatcher{reserved.dispatcher})
		otherOpts = append(otherOpts, reserved.opts...)
	}
	// TODO(mattr): We used to get rt.preferredprotocols here, should we
	// attach these to the context directly?

	traceStore, _ := ctx.Value(vtraceKey).(*ivtrace.Store)
	server, err := iipc.InternalNewServer(ctx, sm, ns, traceStore, otherOpts...)
	if done := ctx.Done(); err == nil && done != nil {
		// Arrange to clean up the server when the parent context is canceled.
		// TODO(mattr): Should we actually do this?  Or just have users clean
		// their own servers up manually?
		go func() {
			<-done
			server.Stop()
		}()
	}
	return server, err
}

func (r *RuntimeX) setNewStreamManager(ctx *context.T, opts ...stream.ManagerOpt) (*context.T, stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return ctx, nil, err
	}
	sm := imanager.InternalNew(rid)
	ctx = context.WithValue(ctx, streamManagerKey, sm)

	// Arrange for the manager to shut itself down when the context is canceled.
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			sm.Shutdown()
		}()
	}

	return ctx, sm, nil
}

func (r *RuntimeX) SetNewStreamManager(ctx *context.T, opts ...stream.ManagerOpt) (*context.T, stream.Manager, error) {
	newctx, sm, err := r.setNewStreamManager(ctx, opts...)
	if err != nil {
		return ctx, nil, err
	}

	// Create a new client since it depends on the stream manager.
	newctx, _, err = r.SetNewClient(newctx)
	if err != nil {
		return ctx, nil, err
	}

	return newctx, sm, nil
}

func (*RuntimeX) StreamManager(ctx *context.T) stream.Manager {
	cl, _ := ctx.Value(streamManagerKey).(stream.Manager)
	return cl
}

func (r *RuntimeX) SetPrincipal(ctx *context.T, principal security.Principal) (*context.T, error) {
	var err error
	newctx := ctx

	newctx = context.WithValue(newctx, principalKey, principal)

	// TODO(mattr, suharshs): The stream manager holds a cache of vifs
	// which were negotiated with the principal, so we replace it here when the
	// principal changes.  However we should negotiate the vif with a
	// random principal and then we needn't replace this here.
	if newctx, _, err = r.setNewStreamManager(newctx); err != nil {
		return ctx, err
	}
	if newctx, _, err = r.setNewNamespace(newctx, r.Namespace(ctx).Roots()...); err != nil {
		return ctx, err
	}
	if newctx, _, err = r.SetNewClient(newctx); err != nil {
		return ctx, err
	}

	return newctx, nil
}

func (*RuntimeX) Principal(ctx *context.T) security.Principal {
	p, _ := ctx.Value(principalKey).(security.Principal)
	return p
}

func (*RuntimeX) SetNewClient(ctx *context.T, opts ...ipc.ClientOpt) (*context.T, ipc.Client, error) {
	// TODO(mattr, suharshs):  Currently there are a lot of things that can come in as opts.
	// Some of them will be removed as opts and simply be pulled from the context instead
	// these are:
	// stream.Manager, Namespace, LocalPrincipal
	sm, _ := ctx.Value(streamManagerKey).(stream.Manager)
	ns, _ := ctx.Value(namespaceKey).(naming.Namespace)
	p, _ := ctx.Value(principalKey).(security.Principal)
	protocols, _ := ctx.Value(protocolsKey).([]string)

	// TODO(mattr, suharshs): Some will need to ba accessible from the
	// client so that we can replace the client transparantly:
	// VCSecurityLevel, PreferredProtocols
	// Currently we are ignoring these and the settings will be lost in some cases.
	// We should try to retrieve them from the client currently attached to the context
	// where possible.
	otherOpts := append([]ipc.ClientOpt{}, opts...)

	// Note we always add DialTimeout, so we don't have to worry about replicating the option.
	otherOpts = append(otherOpts, vc.LocalPrincipal{p}, &imanager.DialTimeout{5 * time.Minute}, options.PreferredProtocols(protocols))

	client, err := iipc.InternalNewClient(sm, ns, otherOpts...)
	if err == nil {
		ctx = context.WithValue(ctx, clientKey, client)
	}
	return ctx, client, err
}

func (*RuntimeX) Client(ctx *context.T) ipc.Client {
	cl, _ := ctx.Value(clientKey).(ipc.Client)
	return cl
}

func (*RuntimeX) SetNewSpan(ctx *context.T, name string) (*context.T, vtrace.Span) {
	return ivtrace.WithNewSpan(ctx, name)
}

func (*RuntimeX) Span(ctx *context.T) vtrace.Span {
	return ivtrace.FromContext(ctx)
}

func (*RuntimeX) setNewNamespace(ctx *context.T, roots ...string) (*context.T, naming.Namespace, error) {
	ns, err := namespace.New(roots...)
	if err == nil {
		ctx = context.WithValue(ctx, namespaceKey, ns)
	}
	return ctx, ns, err
}

func (r *RuntimeX) SetNewNamespace(ctx *context.T, roots ...string) (*context.T, naming.Namespace, error) {
	newctx, ns, err := r.setNewNamespace(ctx, roots...)
	if err != nil {
		return ctx, nil, err
	}

	// Replace the client since it depends on the namespace.
	newctx, _, err = r.SetNewClient(newctx)
	if err != nil {
		return ctx, nil, err
	}

	return newctx, ns, err
}

func (*RuntimeX) Namespace(ctx *context.T) naming.Namespace {
	ns, _ := ctx.Value(namespaceKey).(naming.Namespace)
	return ns
}

func (*RuntimeX) SetNewLogger(ctx *context.T, name string, opts ...vlog.LoggingOpts) (*context.T, vlog.Logger, error) {
	logger, err := vlog.NewLogger(name, opts...)
	if err == nil {
		ctx = context.WithValue(ctx, loggerKey, logger)
	}
	return ctx, logger, err
}

func (*RuntimeX) Logger(ctx *context.T) vlog.Logger {
	logger, _ := ctx.Value(loggerKey).(vlog.Logger)
	return logger
}

func (*RuntimeX) VtraceStore(ctx *context.T) vtrace.Store {
	traceStore, _ := ctx.Value(vtraceKey).(vtrace.Store)
	return traceStore
}

type reservedNameDispatcher struct {
	dispatcher ipc.Dispatcher
	opts       []ipc.ServerOpt
}

// TODO(mattr): Get this from the profile instead, then remove this
// method from the interface.
func (*RuntimeX) SetReservedNameDispatcher(ctx *context.T, server ipc.Dispatcher, opts ...ipc.ServerOpt) *context.T {
	return context.WithValue(ctx, reservedNameKey, &reservedNameDispatcher{server, opts})
}

func (*RuntimeX) SetPreferredProtocols(ctx *context.T, protocols []string) *context.T {
	return context.WithValue(ctx, protocolsKey, protocols)
}

package rt

import (
	"fmt"
	"math/rand"
	"time"

	iipc "v.io/core/veyron/runtimes/google/ipc"
	imanager "v.io/core/veyron/runtimes/google/ipc/stream/manager"
	"v.io/core/veyron/runtimes/google/ipc/stream/vc"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/i18n"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vtrace"
)

func (rt *vrt) NewClient(opts ...ipc.ClientOpt) (ipc.Client, error) {
	rt.mu.Lock()
	sm := rt.sm[0]
	rt.mu.Unlock()
	ns := rt.ns
	var otherOpts []ipc.ClientOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case options.StreamManager:
			sm = topt.Manager
		case options.Namespace:
			ns = topt.Namespace
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	otherOpts = append(otherOpts, vc.LocalPrincipal{rt.principal}, &imanager.DialTimeout{5 * time.Minute}, rt.preferredProtocols)

	dc, err := iipc.InternalNewDischargeClient(sm, ns, rt.NewContext(), otherOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create discharge-client: %v", err)
	}
	return iipc.InternalNewClient(sm, ns, append(otherOpts, dc)...)
}

func (rt *vrt) Client() ipc.Client {
	return rt.client
}

func (rt *vrt) NewContext() *context.T {
	ctx := context.NewUninitializedContext(rt)
	ctx = i18n.ContextWithLangID(ctx, rt.lang)
	ctx = verror2.ContextWithComponentName(ctx, rt.program)

	sr := rt.flags.Vtrace.SampleRate
	forceCollect := sr > 0.0 && (sr >= 1.0 || rand.Float64() < sr)
	ctx, _ = ivtrace.WithNewRootSpan(ctx, rt.traceStore, forceCollect)

	return ctx
}

func (rt *vrt) WithNewSpan(ctx *context.T, name string) (*context.T, vtrace.Span) {
	return ivtrace.WithNewSpan(ctx, name)
}

func (rt *vrt) SpanFromContext(ctx *context.T) vtrace.Span {
	return ivtrace.FromContext(ctx)
}

func (rt *vrt) NewServer(opts ...ipc.ServerOpt) (ipc.Server, error) {
	rt.mu.Lock()
	// Create a new RoutingID (and StreamManager) for each server, except
	// the first one.  The reasoning is for the first server to share the
	// RoutingID (and StreamManager) with Clients.
	//
	// TODO(ashankar/caprita): special-casing the first server is ugly, and
	// has diminished practical benefits since the first server in practice
	// is the app cycle manager server.  If the goal of sharing connections
	// between Clients and Servers in the same Runtime is still important,
	// we need to think of other ways to achieve it.
	sm := rt.sm[0]
	rt.nServers++
	nServers := rt.nServers
	rt.mu.Unlock()
	if nServers > 1 {
		var err error
		sm, err = rt.NewStreamManager()
		if err != nil {
			return nil, fmt.Errorf("failed to create ipc/stream/Manager: %v", err)
		}
	}
	ns := rt.ns
	var otherOpts []ipc.ServerOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case options.Namespace:
			ns = topt
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	// Add the option that provides the principal to the server.
	otherOpts = append(otherOpts, vc.LocalPrincipal{rt.principal})
	if rt.reservedDisp != nil {
		ropts := options.ReservedNameDispatcher{rt.reservedDisp}
		otherOpts = append(otherOpts, ropts)
		otherOpts = append(otherOpts, rt.reservedOpts...)
	}

	// Add the preferred protocols from the runtime if there are any.
	if len(rt.preferredProtocols) > 0 {
		otherOpts = append(otherOpts, iipc.PreferredServerResolveProtocols(rt.preferredProtocols))
	}

	// Add the option that provides a discharge client to the server.
	// TODO(cnicolaou): extend the timeout when parallel connections are
	// going.
	ctx := rt.NewContext()
	dc, err := iipc.InternalNewDischargeClient(sm, ns, ctx, vc.LocalPrincipal{rt.principal}, &imanager.DialTimeout{5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to create discharge-client: %v", err)
	}
	return iipc.InternalNewServer(ctx, sm, ns, rt.traceStore, append(otherOpts, dc)...)
}

func (rt *vrt) NewStreamManager(opts ...stream.ManagerOpt) (stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	sm := imanager.InternalNew(rid)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cleaningUp {
		sm.Shutdown() // For whatever it's worth.
		return nil, errCleaningUp
	}
	rt.sm = append(rt.sm, sm)
	return sm, nil
}

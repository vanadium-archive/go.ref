package rt

import (
	"errors"
	"fmt"

	iipc "veyron.io/veyron/veyron/runtimes/google/ipc"
	imanager "veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	ivtrace "veyron.io/veyron/veyron/runtimes/google/vtrace"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/i18n"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vtrace"
)

// fixedPublicIDStore implements security.PublicIDStore. It embeds a (fixed) PublicID that
// is both the default and the PublicID to be used for any peer. Adding a new PublicID
// to the store is disallowed, and setting the default principal-pattern is a no-op.
type fixedPublicIDStore struct {
	id security.PublicID
}

func (fixedPublicIDStore) Add(id security.PublicID, peerPattern security.BlessingPattern) error {
	return errors.New("adding new PublicIDs is disallowed for this PublicIDStore")
}

func (s fixedPublicIDStore) ForPeer(peer security.PublicID) (security.PublicID, error) {
	return s.id, nil
}

func (s fixedPublicIDStore) DefaultPublicID() (security.PublicID, error) {
	return s.id, nil
}

func (fixedPublicIDStore) SetDefaultBlessingPattern(pattern security.BlessingPattern) error {
	return errors.New("SetDefaultBlessingPattern is disallowed on a fixed PublicIDStore")
}

// localID is an option for passing a PrivateID and PublicIDStore
// to a server or client.
type localID struct {
	id    security.PrivateID
	store security.PublicIDStore
}

func (lID *localID) Sign(message []byte) (security.Signature, error) {
	return lID.id.Sign(message)
}

func (lID *localID) AsClient(server security.PublicID) (security.PublicID, error) {
	return lID.store.ForPeer(server)
}

func (lID *localID) AsServer() (security.PublicID, error) {
	return lID.store.DefaultPublicID()
}

func (*localID) IPCClientOpt() {
	//nologcall
}
func (*localID) IPCStreamVCOpt() {}
func (*localID) IPCServerOpt() {
	//nologcall
}
func (*localID) IPCStreamListenerOpt() {}

// newLocalID returns a localID embedding the runtime's PrivateID and a fixed
// PublicIDStore constructed from the provided PublicID or the runtiume's PublicIDStore
// if the provided PublicID is nil.
func (rt *vrt) newLocalID(id security.PublicID) vc.LocalID {
	lID := &localID{id: rt.id, store: rt.store}
	if id != nil {
		lID.store = fixedPublicIDStore{id}
	}
	return lID
}

func (rt *vrt) NewClient(opts ...ipc.ClientOpt) (ipc.Client, error) {
	rt.mu.Lock()
	sm := rt.sm[0]
	rt.mu.Unlock()
	ns := rt.ns
	var id security.PublicID
	var otherOpts []ipc.ClientOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case options.StreamManager:
			sm = topt.Manager
		case options.Namespace:
			ns = topt.Namespace
		case options.LocalID:
			id = topt.PublicID
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	// Add the option that provides the local identity to the client.
	otherOpts = append(otherOpts, rt.newLocalID(id), vc.LocalPrincipal{rt.principal})
	return iipc.InternalNewClient(sm, ns, otherOpts...)
}

func (rt *vrt) Client() ipc.Client {
	return rt.client
}

func (rt *vrt) NewContext() context.T {
	ctx := iipc.InternalNewContext(rt)
	ctx = i18n.ContextWithLangID(ctx, rt.lang)
	ctx = verror2.ContextWithComponentName(ctx, rt.program)
	ctx, _ = ivtrace.WithNewSpan(ctx, "") // Initial span has empty name.
	return ctx
}

func (rt *vrt) WithNewSpan(ctx context.T, name string) (context.T, vtrace.Span) {
	return ivtrace.WithNewSpan(ctx, name)
}

func (rt *vrt) SpanFromContext(ctx context.T) vtrace.Span {
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
	var id security.PublicID
	var otherOpts []ipc.ServerOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case options.Namespace:
			ns = topt
		case options.LocalID:
			id = topt.PublicID
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	// Add the option that provides the local identity to the server.
	otherOpts = append(otherOpts, rt.newLocalID(id), vc.LocalPrincipal{rt.principal})
	ctx := rt.NewContext()

	return iipc.InternalNewServer(ctx, sm, ns, otherOpts...)
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

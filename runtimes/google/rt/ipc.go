package rt

import (
	"errors"
	"fmt"

	iipc "veyron.io/veyron/veyron/runtimes/google/ipc"
	imanager "veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	ivtrace "veyron.io/veyron/veyron/runtimes/google/vtrace"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
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

func (*localID) IPCClientOpt()         {}
func (*localID) IPCStreamVCOpt()       {}
func (*localID) IPCServerOpt()         {}
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
	sm := rt.sm
	ns := rt.ns
	var id security.PublicID
	var otherOpts []ipc.ClientOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.StreamManagerOpt:
			sm = topt.Manager
		case veyron2.NamespaceOpt:
			ns = topt.Namespace
		case veyron2.LocalIDOpt:
			id = topt.PublicID
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	// Add the option that provides the local identity to the client.
	otherOpts = append(otherOpts, rt.newLocalID(id))

	return iipc.InternalNewClient(sm, ns, otherOpts...)
}

func (rt *vrt) Client() ipc.Client {
	return rt.client
}

func (rt *vrt) NewContext() context.T {
	ctx, _ := ivtrace.WithNewSpan(iipc.InternalNewContext(rt), "Root")
	return ctx
}

func (rt *vrt) WithNewSpan(ctx context.T, name string) (context.T, vtrace.Span) {
	return ivtrace.WithNewSpan(ctx, name)
}

func (rt *vrt) SpanFromContext(ctx context.T) vtrace.Span {
	return ivtrace.FromContext(ctx)
}

func (rt *vrt) NewServer(opts ...ipc.ServerOpt) (ipc.Server, error) {
	var err error

	// Create a new RoutingID (and StreamManager) for each server.
	// Except, in the common case of a process having a single Server,
	// use the same RoutingID (and StreamManager) that is used for Clients.
	rt.mu.Lock()
	sm := rt.sm
	rt.nServers++
	if rt.nServers > 1 {
		sm, err = rt.NewStreamManager()
	}
	rt.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to create ipc/stream/Manager: %v", err)
	}
	// Start the http debug server exactly once for this runtime.
	rt.startHTTPDebugServerOnce()
	ns := rt.ns
	var id security.PublicID
	var otherOpts []ipc.ServerOpt
	addressChooserOpt := &veyron2.AddressChooserOpt{rt.profile.AddressChooser()}
	roamingOpt := &veyron2.RoamingPublisherOpt{rt.publisher, "roaming"}
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.NamespaceOpt:
			ns = topt
		case veyron2.LocalIDOpt:
			id = topt.PublicID
		case *veyron2.AddressChooserOpt:
			addressChooserOpt = topt
		case *veyron2.RoamingPublisherOpt:
			roamingOpt = topt
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	// Add the option that provides the local identity to the server.
	otherOpts = append(otherOpts, rt.newLocalID(id))
	// Add the preferredAddr and roaming opts
	otherOpts = append(otherOpts, addressChooserOpt)
	otherOpts = append(otherOpts, roamingOpt)

	ctx := rt.NewContext()

	return iipc.InternalNewServer(ctx, sm, ns, otherOpts...)
}

func (rt *vrt) NewStreamManager(opts ...stream.ManagerOpt) (stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	sm := imanager.InternalNew(rid)
	rt.debug.RegisterStreamManager(rid, sm)
	return sm, nil
}

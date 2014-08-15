package rt

import (
	"errors"
	"fmt"

	iipc "veyron/runtimes/google/ipc"
	imanager "veyron/runtimes/google/ipc/stream/manager"
	"veyron/runtimes/google/ipc/stream/vc"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/ipc/stream"
	"veyron2/naming"
	"veyron2/security"
)

// fixedPublicIDStore implements security.PublicIDStore. It embeds a (fixed) PublicID that
// is both the default and the PublicID to be used for any peer. Adding a new PublicID
// to the store is disallowed, and setting the default principal-pattern is a no-op.
type fixedPublicIDStore struct {
	id security.PublicID
}

func (fixedPublicIDStore) Add(id security.PublicID, peerPattern security.PrincipalPattern) error {
	return errors.New("adding new PublicIDs is disallowed for this PublicIDStore")
}

func (s fixedPublicIDStore) ForPeer(peer security.PublicID) (security.PublicID, error) {
	return s.id, nil
}

func (s fixedPublicIDStore) DefaultPublicID() (security.PublicID, error) {
	return s.id, nil
}

func (fixedPublicIDStore) SetDefaultPrincipalPattern(pattern security.PrincipalPattern) error {
	return errors.New("SetDefaultPrincipalPattern is disallowed on a fixed PublicIDStore")
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
	return iipc.InternalNewContext()
}

func (rt *vrt) TODOContext() context.T {
	return iipc.InternalNewContext()
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
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.NamespaceOpt:
			ns = topt
		case veyron2.LocalIDOpt:
			id = topt.PublicID
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	// Add the option that provides the local identity to the server.
	otherOpts = append(otherOpts, rt.newLocalID(id))

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

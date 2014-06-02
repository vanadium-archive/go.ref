package rt

import (
	iipc "veyron/runtimes/google/ipc"
	imanager "veyron/runtimes/google/ipc/stream/manager"

	"veyron2"
	"veyron2/ipc"
	"veyron2/ipc/stream"
	"veyron2/naming"
)

func (rt *vrt) NewClient(opts ...ipc.ClientOpt) (ipc.Client, error) {
	sm := rt.sm
	mt := rt.mt
	id := rt.id
	var otherOpts []ipc.ClientOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.StreamManagerOpt:
			sm = topt.Manager
		case veyron2.MountTableOpt:
			mt = topt.MountTable
		case veyron2.LocalIDOpt:
			id = topt
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	if id.PrivateID != nil {
		otherOpts = append(otherOpts, id)
	}
	return iipc.InternalNewClient(sm, mt, otherOpts...)
}

func (rt *vrt) Client() ipc.Client {
	return rt.client
}

func (rt *vrt) NewContext() ipc.Context {
	return iipc.InternalNewContext()
}

func (rt *vrt) TODOContext() ipc.Context {
	return iipc.InternalNewContext()
}

func (rt *vrt) NewServer(opts ...ipc.ServerOpt) (ipc.Server, error) {
	// Start the http debug server exactly once for this process
	rt.startHTTPDebugServerOnce()

	sm := rt.sm
	mt := rt.mt
	idSpecified := false
	var otherOpts []ipc.ServerOpt
	for _, opt := range opts {
		switch topt := opt.(type) {
		case veyron2.StreamManagerOpt:
			sm = topt
		case veyron2.MountTableOpt:
			mt = topt
		case veyron2.LocalIDOpt:
			idSpecified = true
			otherOpts = append(otherOpts, opt)
		default:
			otherOpts = append(otherOpts, opt)
		}
	}
	if !idSpecified && rt.id.PrivateID != nil {
		otherOpts = append(otherOpts, rt.id)
	}
	return iipc.InternalNewServer(sm, mt, otherOpts...)
}

func (rt *vrt) NewStreamManager(opts ...stream.ManagerOpt) (stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	rt.rid = rid
	return imanager.InternalNew(rt.rid), nil
}

func (rt *vrt) StreamManager() stream.Manager {
	return rt.sm
}

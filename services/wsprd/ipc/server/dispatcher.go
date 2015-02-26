package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"v.io/core/veyron/services/wsprd/lib"

	"v.io/v23/ipc"
	"v.io/v23/security"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/verror"
	"v.io/v23/vlog"
)

type flowFactory interface {
	createFlow() *Flow
	cleanupFlow(id int32)
}

type invokerFactory interface {
	createInvoker(handle int32, signature []signature.Interface, hasGlobber bool) (ipc.Invoker, error)
}

type authFactory interface {
	createAuthorizer(handle int32, hasAuthorizer bool) (security.Authorizer, error)
}

type lookupIntermediateReply struct {
	Handle        int32
	HasAuthorizer bool
	HasGlobber    bool
	Signature     string
	Err           *verror.Standard
}

type lookupReply struct {
	Handle        int32
	HasAuthorizer bool
	HasGlobber    bool
	Signature     []signature.Interface
	Err           *verror.Standard
}

type dispatcherRequest struct {
	ServerID uint32 `json:"serverId"`
	Suffix   string `json:"suffix"`
}

// dispatcher holds the invoker and the authorizer to be used for lookup.
type dispatcher struct {
	mu                 sync.Mutex
	serverID           uint32
	flowFactory        flowFactory
	invokerFactory     invokerFactory
	authFactory        authFactory
	outstandingLookups map[int32]chan lookupReply
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

// newDispatcher is a dispatcher factory.
func newDispatcher(serverID uint32, flowFactory flowFactory, invokerFactory invokerFactory, authFactory authFactory) *dispatcher {
	return &dispatcher{
		serverID:           serverID,
		flowFactory:        flowFactory,
		invokerFactory:     invokerFactory,
		authFactory:        authFactory,
		outstandingLookups: make(map[int32]chan lookupReply),
	}
}

func (d *dispatcher) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, ch := range d.outstandingLookups {
		verr := verror.Convert(verror.ErrInternal, nil, fmt.Errorf("Cleaning up dispatcher")).(verror.Standard)
		ch <- lookupReply{Err: &verr}
	}
}

// Lookup implements dispatcher interface Lookup.
func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	flow := d.flowFactory.createFlow()
	d.mu.Lock()
	ch := make(chan lookupReply, 1)
	d.outstandingLookups[flow.ID] = ch
	d.mu.Unlock()

	message := dispatcherRequest{
		ServerID: d.serverID,
		Suffix:   suffix,
	}
	if err := flow.Writer.Send(lib.ResponseDispatcherLookup, message); err != nil {
		verr := verror.Convert(verror.ErrInternal, nil, err).(verror.Standard)
		ch <- lookupReply{Err: &verr}
	}
	reply := <-ch

	d.mu.Lock()
	delete(d.outstandingLookups, flow.ID)
	d.mu.Unlock()

	d.flowFactory.cleanupFlow(flow.ID)

	if reply.Err != nil {
		return nil, nil, reply.Err
	}
	if reply.Handle < 0 {
		return nil, nil, verror.New(verror.ErrNoExist, nil, "Dispatcher", suffix)
	}

	invoker, err := d.invokerFactory.createInvoker(reply.Handle, reply.Signature, reply.HasGlobber)
	if err != nil {
		return nil, nil, err
	}
	auth, err := d.authFactory.createAuthorizer(reply.Handle, reply.HasAuthorizer)
	if err != nil {
		return nil, nil, err
	}

	return invoker, auth, nil
}

func (d *dispatcher) handleLookupResponse(id int32, data string) {
	d.mu.Lock()
	ch := d.outstandingLookups[id]
	d.mu.Unlock()

	if ch == nil {
		d.flowFactory.cleanupFlow(id)
		vlog.Errorf("unknown invoke request for flow: %d", id)
		return
	}

	var intermediateReply lookupIntermediateReply
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&intermediateReply); err != nil {
		err2 := verror.Convert(verror.ErrInternal, nil, err).(verror.Standard)
		intermediateReply = lookupIntermediateReply{Err: &err2}
		vlog.Errorf("unmarshaling invoke request failed: %v, %s", err, data)
	}

	reply := lookupReply{
		Handle:        intermediateReply.Handle,
		HasAuthorizer: intermediateReply.HasAuthorizer,
		HasGlobber:    intermediateReply.HasGlobber,
		Err:           intermediateReply.Err,
	}
	if reply.Err == nil && intermediateReply.Signature != "" {
		if err := lib.VomDecode(intermediateReply.Signature, &reply.Signature); err != nil {
			err2 := verror.Convert(verror.ErrInternal, nil, err).(verror.Standard)
			reply.Err = &err2
		}
	}
	ch <- reply
}

// StopServing implements dispatcher StopServing.
func (*dispatcher) StopServing() {
}

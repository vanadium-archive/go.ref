package server

import (
	"bytes"
	"encoding/json"
	"sync"

	"veyron.io/wspr/veyron/services/wsprd/lib"
	"veyron.io/wspr/veyron/services/wsprd/signature"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"
)

type flowFactory interface {
	createFlow() *Flow
	cleanupFlow(id int64)
}

type invokerFactory interface {
	createInvoker(handle int64, signature signature.JSONServiceSignature, label security.Label) (ipc.Invoker, error)
}

type authFactory interface {
	createAuthorizer(handle int64, hasAuthorizer bool) (security.Authorizer, error)
}

type lookupReply struct {
	Handle        int64
	HasAuthorizer bool
	Label         security.Label
	Signature     signature.JSONServiceSignature
	Err           *verror2.Standard
}

type dispatcherRequest struct {
	ServerID uint64 `json:"serverId"`
	Suffix   string `json:"suffix"`
}

// dispatcher holds the invoker and the authorizer to be used for lookup.
type dispatcher struct {
	mu                 sync.Mutex
	serverID           uint64
	flowFactory        flowFactory
	invokerFactory     invokerFactory
	authFactory        authFactory
	logger             vlog.Logger
	outstandingLookups map[int64]chan lookupReply
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

// newDispatcher is a dispatcher factory.
func newDispatcher(serverID uint64, flowFactory flowFactory, invokerFactory invokerFactory, authFactory authFactory, logger vlog.Logger) *dispatcher {
	return &dispatcher{
		serverID:           serverID,
		flowFactory:        flowFactory,
		invokerFactory:     invokerFactory,
		authFactory:        authFactory,
		logger:             logger,
		outstandingLookups: make(map[int64]chan lookupReply),
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
		ch <- lookupReply{Err: verror2.Convert(verror2.Internal, nil, err).(*verror2.Standard)}
	}
	request := <-ch

	d.mu.Lock()
	delete(d.outstandingLookups, flow.ID)
	d.mu.Unlock()

	d.flowFactory.cleanupFlow(flow.ID)

	if request.Err != nil {
		return nil, nil, request.Err
	}

	if request.Handle < 0 {
		return nil, nil, verror2.Make(verror2.NoExist, nil, "Dispatcher", suffix)
	}

	invoker, err := d.invokerFactory.createInvoker(request.Handle, request.Signature, request.Label)
	if err != nil {
		return nil, nil, err
	}

	auth, err := d.authFactory.createAuthorizer(request.Handle, request.HasAuthorizer)

	return invoker, auth, err
}

func (d *dispatcher) handleLookupResponse(id int64, data string) {
	d.mu.Lock()
	ch := d.outstandingLookups[id]
	d.mu.Unlock()

	if ch == nil {
		d.flowFactory.cleanupFlow(id)
		d.logger.Errorf("unknown invoke request for flow: %d", id)
		return
	}

	var request lookupReply
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if err := decoder.Decode(&request); err != nil {
		request = lookupReply{Err: verror2.Convert(verror2.Internal, nil, err).(*verror2.Standard)}
		d.logger.Errorf("unmarshaling invoke request failed: %v, %s", err, data)
	}
	ch <- request
}

// StopServing implements dispatcher StopServing.
func (*dispatcher) StopServing() {
}

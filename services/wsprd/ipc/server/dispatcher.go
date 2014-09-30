package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"veyron.io/veyron/veyron/services/wsprd/lib"
	"veyron.io/veyron/veyron/services/wsprd/signature"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

type flowFactory interface {
	createFlow() *Flow
	cleanupFlow(id int64)
}

type invokerFactory interface {
	createInvoker(handle int64, signature signature.JSONServiceSignature) (ipc.Invoker, error)
}

type authFactory interface {
	createAuthorizer(handle int64, hasAuthorizer bool) (security.Authorizer, error)
}

type lookupReply struct {
	Handle        int64
	HasAuthorizer bool
	Signature     signature.JSONServiceSignature
	Err           *verror.Standard
}

type dispatcherRequest struct {
	ServerID uint64 `json:"serverId"`
	Suffix   string `json:"suffix"`
	Method   string `json:"method"`
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
func (d *dispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	flow := d.flowFactory.createFlow()
	d.mu.Lock()
	ch := make(chan lookupReply, 1)
	d.outstandingLookups[flow.ID] = ch
	d.mu.Unlock()

	message := dispatcherRequest{
		ServerID: d.serverID,
		Suffix:   suffix,
		Method:   lib.LowercaseFirstCharacter(method),
	}
	if err := flow.Writer.Send(lib.ResponseDispatcherLookup, message); err != nil {
		ch <- lookupReply{
			Err: &verror.Standard{
				ID:  verror.Internal,
				Msg: fmt.Sprintf("could not marshal the method call data: %v", err),
			},
		}
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
		return nil, nil, verror.NoExistf("ipc: dispatcher for %s not found", suffix)
	}

	invoker, err := d.invokerFactory.createInvoker(request.Handle, request.Signature)
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
		request = lookupReply{
			Err: &verror.Standard{
				ID:  verror.Internal,
				Msg: fmt.Sprintf("could not unmarshal invoke request: %v", err),
			},
		}
		d.logger.Errorf("unmarshaling invoke request failed: %v", err)
	}
	ch <- request
}

// StopServing implements dispatcher StopServing.
func (*dispatcher) StopServing() {
}

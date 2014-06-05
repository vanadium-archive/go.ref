// An implementation of a server for WSPR

package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"veyron2"
	"veyron2/ipc"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"
)

type flow struct {
	id     int64
	writer clientWriter
}

type serverHelper interface {
	createNewFlow(server *server, sender sender) *flow

	cleanupFlow(id int64)

	getLogger() vlog.Logger

	rt() veyron2.Runtime
}

type server struct {
	sync.Mutex
	server                    ipc.Server
	endpoint                  string
	id                        uint64
	helper                    serverHelper
	veyronProxy               string
	outstandingServerRequests map[int64]chan *serverRPCReply
}

func newServer(id uint64, veyronProxy string, helper serverHelper) (*server, error) {
	server := &server{
		id:                        id,
		helper:                    helper,
		veyronProxy:               veyronProxy,
		outstandingServerRequests: make(map[int64]chan *serverRPCReply),
	}

	manager, err := helper.rt().NewStreamManager()

	if err != nil {
		return nil, err
	}

	server.server, err = helper.rt().NewServer(veyron2.StreamManager(manager))

	if err != nil {
		return nil, err
	}
	return server, nil
}

// remoteInvokeFunc is a type of function that can invoke a remote method and
// communicate the result back via a channel to the caller
type remoteInvokeFunc func(methodName string, args []interface{}, call ipc.ServerCall) <-chan *serverRPCReply

func proxyStream(stream ipc.Stream, w clientWriter) {
	var item interface{}
	for err := stream.Recv(&item); err == nil; err = stream.Recv(&item) {
		data := response{Type: responseStream, Message: item}
		if err := vom.ObjToJSON(w, vom.ValueOf(data)); err != nil {
			w.sendError(verror.Internalf("error marshalling stream: %v:", err))
			return
		}
		if err := w.FinishMessage(); err != nil {
			w.getLogger().VI(2).Info("WSPR: error finishing message", err)
			return
		}
	}

	if err := vom.ObjToJSON(w, vom.ValueOf(response{Type: responseStreamClose})); err != nil {
		w.sendError(verror.Internalf("error closing stream: %v:", err))
		return
	}
	if err := w.FinishMessage(); err != nil {
		w.getLogger().VI(2).Info("WSPR: error finishing message", err)
		return
	}
}

func (s *server) createRemoteInvokerFunc(serviceName string) remoteInvokeFunc {
	return func(methodName string, args []interface{}, call ipc.ServerCall) <-chan *serverRPCReply {
		flow := s.helper.createNewFlow(s, senderWrapper{stream: call})
		replyChan := make(chan *serverRPCReply, 1)
		s.Lock()
		s.outstandingServerRequests[flow.id] = replyChan
		s.Unlock()
		context := serverRPCRequestContext{
			Suffix: call.Suffix(),
			Name:   call.Name(),
		}
		// Send a invocation request to JavaScript
		message := serverRPCRequest{
			ServerId:    s.id,
			ServiceName: serviceName,
			Method:      lowercaseFirstCharacter(methodName),
			Args:        args,
			Context:     context,
		}

		data := response{Type: responseServerRequest, Message: message}
		if err := vom.ObjToJSON(flow.writer, vom.ValueOf(data)); err != nil {
			// Error in marshaling, pass the error through the channel immediately
			replyChan <- &serverRPCReply{nil,
				&verror.Standard{
					ID:  verror.Internal,
					Msg: fmt.Sprintf("could not marshal the method call data: %v", err)},
			}
			return replyChan
		}
		if err := flow.writer.FinishMessage(); err != nil {
			replyChan <- &serverRPCReply{nil,
				&verror.Standard{
					ID:  verror.Internal,
					Msg: fmt.Sprintf("WSPR: error finishing message: %v", err)},
			}
			return replyChan
		}

		s.helper.getLogger().VI(3).Infof("request received to call method %q on "+
			"JavaScript server %q with args %v, MessageId %d was assigned.",
			methodName, serviceName, args, flow.id)

		go proxyStream(call, flow.writer)
		return replyChan
	}
}

func (s *server) register(name string, sig JSONServiceSignature) error {
	serviceSig, err := sig.ServiceSignature()
	if err != nil {
		return err
	}

	remoteInvokeFunc := s.createRemoteInvokerFunc(name)
	invoker, err := newInvoker(serviceSig, remoteInvokeFunc)

	if err != nil {
		return err
	}
	dispatcher := newDispatcher(invoker, security.NewACLAuthorizer(
		security.ACL{security.AllPrincipals: security.AllLabels},
	))

	if err := s.server.Register(name, dispatcher); err != nil {
		return err
	}

	return nil
}

func (s *server) publish(name string) (string, error) {
	s.Lock()
	defer s.Unlock()
	if s.endpoint == "" {
		endpoint, err := s.server.Listen("veyron", s.veyronProxy)

		if err != nil {
			return "", err
		}
		s.endpoint = endpoint.String()
	}
	if err := s.server.Publish(name); err != nil {
		return "", err
	}
	s.helper.getLogger().VI(0).Infof("endpoint is %s", s.endpoint)
	return s.endpoint, nil
}

func (s *server) handleServerResponse(id int64, data string) {
	s.Lock()
	ch := s.outstandingServerRequests[id]
	delete(s.outstandingServerRequests, id)
	s.Unlock()
	if ch == nil {
		s.helper.getLogger().VI(0).Infof("unexpected result from JavaScript. No channel "+
			"for MessageId: %d exists. Ignoring the results.", id)
		//Ignore unknown responses that don't belong to any channel
		return
	}
	// Decode the result and send it through the channel
	var serverReply serverRPCReply
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	if decoderErr := decoder.Decode(&serverReply); decoderErr != nil {
		err := verror.Standard{
			ID:  verror.Internal,
			Msg: fmt.Sprintf("could not unmarshal the result from the server: %v", decoderErr),
		}
		serverReply = serverRPCReply{nil, &err}
	}

	s.helper.getLogger().VI(3).Infof("response received from JavaScript server for "+
		"MessageId %d with result %v", id, serverReply)
	s.helper.cleanupFlow(id)
	ch <- &serverReply
}

func (s *server) Stop() {
	result := serverRPCReply{
		Results: []interface{}{nil},
		Err: &verror.Standard{
			ID:  verror.Aborted,
			Msg: "timeout",
		},
	}
	s.Lock()
	defer s.Unlock()
	for _, ch := range s.outstandingServerRequests {
		select {
		case ch <- &result:
		default:
		}
	}
	s.outstandingServerRequests = make(map[int64]chan *serverRPCReply)
	s.server.Stop()
}

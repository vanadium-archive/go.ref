package ipc

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	inaming "veyron/runtimes/google/naming"
	isecurity "veyron/runtimes/google/security"
	"veyron/runtimes/google/security/wire"

	"veyron2"
	"veyron2/ipc"
	"veyron2/ipc/stream"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/verror"
	"veyron2/vlog"
	"veyron2/vom"
)

var (
	errNoDispatcher  = verror.NotFoundf("ipc: dispatcher not found")
	errWrongNumArgs  = verror.BadProtocolf("ipc: wrong number of input arguments")
	errServerStopped = verror.Abortedf("ipc: server is stopped")
)

func errNotAuthorized(err error) verror.E {
	return verror.NotAuthorizedf("ipc: not authorized(%v)", err)
}

type server struct {
	sync.Mutex
	streamMgr    stream.Manager       // stream manager to listen for new flows.
	disptrie     *disptrie            // dispatch trie for method dispatching.
	publisher    Publisher            // publisher to publish mounttable mounts.
	listenerOpts []stream.ListenerOpt // listener opts passed to Listen.
	listeners    []stream.Listener    // listeners created by Listen.
	active       sync.WaitGroup       // active goroutines we've spawned.
	stopped      bool                 // whether the server has been stopped.
	mt           naming.MountTable
	publishOpt   veyron2.ServerPublishOpt // which endpoints to publish
}

func InternalNewServer(streamMgr stream.Manager, mt naming.MountTable, opts ...ipc.ServerOpt) (ipc.Server, error) {
	s := &server{
		streamMgr: streamMgr,
		disptrie:  newDisptrie(),
		publisher: InternalNewPublisher(mt, publishPeriod),
		mt:        mt,
	}
	for _, opt := range opts {
		switch opt := opt.(type) {
		case stream.ListenerOpt:
			// Collect all ServerOpts that are also ListenerOpts.
			s.listenerOpts = append(s.listenerOpts, opt)
		case veyron2.ServerPublishOpt:
			s.publishOpt = opt
		}
	}
	return s, nil
}

func (s *server) Register(prefix string, disp ipc.Dispatcher) error {
	s.Lock()
	defer s.Unlock()
	if s.stopped {
		return errServerStopped
	}
	return s.disptrie.Register(prefix, disp)
}

func (s *server) Published() ([]string, error) {
	s.Lock()
	defer s.Unlock()
	if s.stopped {
		return nil, errServerStopped
	}
	return s.publisher.Published(), nil
}

// resolveToAddress will try to resolve the input to an address using the
// mount table, if the input is not already an address.
func (s *server) resolveToAddress(address string) string {
	if _, err := inaming.NewEndpoint(address); err == nil {
		return address
	}
	if s.mt == nil {
		return address
	}
	names, err := s.mt.Resolve(address)
	if err != nil {
		return address
	}
	for _, n := range names {
		address, suffix := naming.SplitAddressName(n)
		if len(suffix) != 0 {
			continue
		}
		if _, err := inaming.NewEndpoint(address); err == nil {
			return address
		}
	}
	return address
}

func (s *server) Listen(protocol, address string) (naming.Endpoint, error) {
	s.Lock()
	// Shortcut if the server is stopped, to avoid needlessly creating a
	// listener.
	if s.stopped {
		s.Unlock()
		return nil, errServerStopped
	}
	s.Unlock()
	if protocol == inaming.Network {
		address = s.resolveToAddress(address)
	}
	ln, ep, err := s.streamMgr.Listen(protocol, address, s.listenerOpts...)
	if err != nil {
		vlog.Errorf("ipc: Listen on %v %v failed: %v", protocol, address, err)
		return nil, err
	}
	s.Lock()
	if s.stopped {
		s.Unlock()
		// Ignore error return since we can't really do much about it.
		ln.Close()
		return nil, errServerStopped
	}
	s.listeners = append(s.listeners, ln)
	// We have a single goroutine per listener to accept new flows.  Each flow is
	// served from its own goroutine.
	s.active.Add(1)
	go func(ln stream.Listener, ep naming.Endpoint) {
		defer vlog.VI(1).Infof("ipc: Stopped listening on %v", ep)
		for {
			flow, err := ln.Accept()
			if err != nil {
				vlog.VI(10).Infof("ipc: Accept on %v %v failed: %v", protocol, address, err)
				s.active.Done()
				return
			}
			s.active.Add(1)
			go func(flow stream.Flow) {
				if err := newFlowServer(flow, s).serve(); err != nil {
					// TODO(caprita): Logging errors here is
					// too spammy. For example, "not
					// authorized" errors shouldn't be
					// logged as server errors.
					vlog.Errorf("Flow serve on (%v, %v) failed: %v", protocol, address, err)
				}
				s.active.Done()
			}(flow)
		}
	}(ln, ep)
	var publishEP string
	if s.publishOpt == veyron2.PublishAll || len(s.listeners) == 1 {
		publishEP = naming.JoinAddressName(ep.String(), "")
	}
	s.Unlock()
	if len(publishEP) > 0 {
		s.publisher.AddServer(publishEP)
	}
	return ep, nil
}

func (s *server) Publish(name string) error {
	s.publisher.AddName(name)
	return nil
}

func (s *server) Stop() error {
	s.Lock()
	if s.stopped {
		s.Unlock()
		return nil
	}
	s.stopped = true
	s.Unlock()

	// Note, It's safe to Stop/WaitForStop on the publisher outside of the
	// server lock, since publisher is safe for concurrent access.

	// Stop the publisher, which triggers unmounting of published names.
	s.publisher.Stop()
	// Wait for the publisher to be done unmounting before we can proceed to
	// close the listeners (to minimize the number of mounted names pointing
	// to endpoint that are no longer serving).
	//
	// TODO(caprita): See if make sense to fail fast on rejecting
	// connections once listeners are closed, and parallelize the publisher
	// and listener shutdown.
	s.publisher.WaitForStop()

	s.Lock()
	// Close all listeners.  No new flows will be accepted, while in-flight
	// flows will continue until they terminate naturally.
	nListeners := len(s.listeners)
	errCh := make(chan error, nListeners)
	for _, ln := range s.listeners {
		go func(ln stream.Listener) {
			errCh <- ln.Close()
		}(ln)
	}
	s.Unlock()
	var firstErr error
	for i := 0; i < nListeners; i++ {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// At this point, we are guaranteed that no new requests are going to be
	// accepted.

	// Wait for the publisher and active listener + flows to finish.
	s.active.Wait()

	// Once all outstanding requests are done, we can clean up the rest of
	// the state.
	s.disptrie.Stop()

	return firstErr
}

// flowServer implements the RPC server-side protocol for a single RPC, over a
// flow that's already connected to the client.
type flowServer struct {
	disptrie *disptrie    // dispatch trie
	server   ipc.Server   // ipc.Server that this flow server belongs to
	dec      *vom.Decoder // to decode requests and args from the client
	enc      *vom.Encoder // to encode responses and results to the client
	flow     stream.Flow  // underlying flow
	// Fields filled in during the server invocation.

	// authorizedRemoteID is the PublicID obtained after authorizing the remoteID
	// of the underlying flow for the current request context.
	authorizedRemoteID   security.PublicID
	method, name, suffix string
	label                security.Label
	discharges           security.CaveatDischargeMap
	deadline             time.Time
	endStreamArgs        bool // are the stream args at EOF?
}

func newFlowServer(flow stream.Flow, server *server) *flowServer {
	return &flowServer{
		server:   server,
		disptrie: server.disptrie,
		// TODO(toddw): Support different codecs
		dec:  vom.NewDecoder(flow),
		enc:  vom.NewEncoder(flow),
		flow: flow,
	}
}

// Vom does not encode untyped nils.
// Consequently, the ipc system does not allow nil results with an interface
// type from server methods.  The one exception being errors.
//
// For now, the following hacky assumptions are made, which will be revisited when
// a decision is made on how untyped nils should be encoded/decoded in
// vom/vom2:
//
// - Server methods return 0 or more results
// - Any values returned by the server that have an interface type are either
//   non-nil or of type error.
func result2vom(res interface{}) vom.Value {
	v := vom.ValueOf(res)
	if !v.IsValid() {
		// Untyped nils are assumed to be nil-errors.
		var boxed verror.E
		return vom.ValueOf(&boxed).Elem()
	}
	if err, iserr := res.(error); iserr {
		// Convert errors to verror since errors are often not
		// serializable via vom/gob (errors.New and fmt.Errorf return a
		// type with no exported fields).
		return vom.ValueOf(verror.Convert(err))
	}
	return v
}

func defaultACL(id security.PublicID) security.ACL {
	if id == nil {
		return nil
	}
	acl := make(security.ACL)
	for _, n := range id.Names() {
		if !strings.HasPrefix(n, wire.UntrustedIDProviderPrefix) {
			acl[security.PrincipalPattern(n+wire.ChainSeparator+security.AllPrincipals)] = security.AllLabels
		}
	}
	return acl
}

func (fs *flowServer) serve() error {
	defer fs.flow.Close()
	results, err := fs.processRequest()
	// Respond to the client with the response header and positional results.
	response := ipc.Response{
		Error:            err,
		EndStreamResults: true,
		NumPosResults:    uint64(len(results)),
	}
	if err := fs.enc.Encode(response); err != nil {
		return verror.BadProtocolf("ipc: response encoding failed: %v", err)
	}
	if response.Error != nil {
		return response.Error
	}
	for ix, res := range results {
		if err := fs.enc.EncodeValue(result2vom(res)); err != nil {
			return verror.BadProtocolf("ipc: result #%d [%T=%v] encoding failed: %v", ix, res, res, err)
		}
	}
	// TODO(ashankar): Should unread data from the flow be drained?
	//
	// Reason to do so:
	// The common stream.Flow implementation (veyron/runtimes/google/ipc/stream/vc/reader.go)
	// uses iobuf.Slices backed by an iobuf.Pool. If the stream is not drained, these
	// slices will not be returned to the pool leading to possibly increased memory usage.
	//
	// Reason to not do so:
	// Draining here will conflict with any Reads on the flow in a separate goroutine
	// (for example, see TestStreamReadTerminatedByServer in full_test.go).
	//
	// For now, go with the reason to not do so as having unread data in the stream
	// should be a rare case.
	return nil
}

func (fs *flowServer) processRequest() ([]interface{}, verror.E) {
	start := time.Now()
	// Set a default timeout before reading from the flow. Without this timeout,
	// a client that sends no request or a partial request will retain the flow
	// indefinitely (and lock up server resources).
	if verr := fs.setDeadline(start.Add(defaultCallTimeout)); verr != nil {
		return nil, verr
	}
	// Decode the initial request.
	var req ipc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return nil, verror.BadProtocolf("ipc: request decoding failed: %v", err)
	}
	fs.method = req.Method
	// Set the appropriate deadline, if specified.
	if req.Timeout == ipc.NoTimeout {
		if verr := fs.setDeadline(time.Time{}); verr != nil {
			return nil, verr
		}
	} else if req.Timeout > 0 {
		if verr := fs.setDeadline(start.Add(time.Duration(req.Timeout))); verr != nil {
			return nil, verr
		}
	}
	// Lookup the invoker.
	invoker, auth, name, suffix, verr := fs.lookup(req.Suffix)
	fs.name = name
	fs.suffix = suffix
	if verr != nil {
		return nil, verr
	}
	// Prepare invoker and decode args.
	numArgs := int(req.NumPosArgs)
	argptrs, label, err := invoker.Prepare(req.Method, numArgs)
	fs.label = label
	// TODO(ataly, ashankar): Populate the "discharges" field from the request object req.
	if err != nil {
		return nil, verror.Convert(err)
	}
	if len(argptrs) != numArgs {
		return nil, errWrongNumArgs
	}
	for ix, argptr := range argptrs {
		if err := fs.dec.Decode(argptr); err != nil {
			return nil, verror.BadProtocolf("ipc: arg %d decoding failed: %v", ix, err)
		}
	}
	// Authorize the PublicID at the remote end of the flow for the request context.
	if remoteID := fs.flow.RemoteID(); remoteID != nil {
		if fs.authorizedRemoteID, err = remoteID.Authorize(isecurity.NewContext(
			isecurity.ContextArgs{
				LocalID:    fs.flow.LocalID(),
				RemoteID:   fs.flow.RemoteID(),
				Method:     fs.method,
				Name:       fs.name,
				Suffix:     fs.suffix,
				Discharges: fs.discharges,
				Label:      fs.label})); err != nil {
			return nil, errNotAuthorized(err)
		}
	}
	// Check application's authorization policy and invoke the method.
	if err := fs.authorize(auth); err != nil {
		// TODO(ataly, ashankar): For privacy reasons, should we hide the authorizer error (err)?
		return nil, errNotAuthorized(fmt.Errorf("%q not authorized for method %q: %v", fs.RemoteID(), fs.Method(), err))
	}
	results, err := invoker.Invoke(req.Method, fs, argptrs)
	return results, verror.Convert(err)
}

// lookup returns the invoker and authorizer responsible for serving the given
// name.  The name is stripped of any leading slashes, and the invoker is looked
// up in the server's dispatcher.  The (stripped) name and dispatch suffix are
// also returned.
func (fs *flowServer) lookup(name string) (ipc.Invoker, security.Authorizer, string, string, verror.E) {
	name = strings.TrimLeft(name, "/")
	disps, suffix := fs.disptrie.Lookup(name)
	for _, disp := range disps {
		invoker, auth, err := disp.Lookup(suffix)
		switch {
		case err != nil:
			return nil, nil, "", "", verror.Convert(err)
		case invoker != nil:
			return invoker, auth, name, suffix, nil
		}
		// The dispatcher doesn't handle this suffix, try the next one.
	}
	return nil, nil, "", "", errNoDispatcher
}

func (fs *flowServer) authorize(auth security.Authorizer) error {
	if auth != nil {
		return auth.Authorize(fs)
	}
	// Since the provided authorizer is nil we create a default IDAuthorizer
	// for the local identity of the flow. This authorizer only authorizes
	// remote identities that have either been blessed by the local identity
	// or have blessed the local identity. (See security.NewACLAuthorizer)
	return security.NewACLAuthorizer(defaultACL(fs.flow.LocalID())).Authorize(fs)
}

// setDeadline sets a deadline on the flow. The flow will be cancelled if it
// is not closed by the specified deadline.
// A zero deadline (time.Time.IsZero) implies that no cancellation is desired.
func (fs *flowServer) setDeadline(deadline time.Time) verror.E {
	fs.deadline = deadline
	if err := fs.flow.SetDeadline(deadline); err != nil {
		return verror.Internalf("ipc: flow SetDeadline failed: %v", err)
	}
	return nil
}

// Send implements the ipc.Stream method.
func (fs *flowServer) Send(item interface{}) error {
	// The empty response header indicates what follows is a streaming result.
	if err := fs.enc.Encode(ipc.Response{}); err != nil {
		return err
	}
	return fs.enc.Encode(item)
}

// Recv implements the ipc.Stream method.
func (fs *flowServer) Recv(itemptr interface{}) error {
	var req ipc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return err
	}
	if req.EndStreamArgs {
		fs.endStreamArgs = true
		return io.EOF
	}
	return fs.dec.Decode(itemptr)
}

// Implementations of ipc.Context methods.

func (fs *flowServer) Server() ipc.Server                            { return fs.server }
func (fs *flowServer) Method() string                                { return fs.method }
func (fs *flowServer) Name() string                                  { return fs.name }
func (fs *flowServer) Suffix() string                                { return fs.suffix }
func (fs *flowServer) Label() security.Label                         { return fs.label }
func (fs *flowServer) CaveatDischarges() security.CaveatDischargeMap { return fs.discharges }
func (fs *flowServer) LocalID() security.PublicID                    { return fs.flow.LocalID() }
func (fs *flowServer) RemoteID() security.PublicID                   { return fs.authorizedRemoteID }
func (fs *flowServer) Deadline() time.Time                           { return fs.deadline }
func (fs *flowServer) LocalAddr() net.Addr                           { return fs.flow.LocalAddr() }
func (fs *flowServer) RemoteAddr() net.Addr                          { return fs.flow.RemoteAddr() }

func (fs *flowServer) IsClosed() bool {
	return fs.flow.IsClosed()
}

func (fs *flowServer) Closed() <-chan struct{} {
	return fs.flow.Closed()
}

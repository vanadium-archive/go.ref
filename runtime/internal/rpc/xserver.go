// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/i18n"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	"v.io/x/ref/lib/apilog"
	"v.io/x/ref/lib/pubsub"
	"v.io/x/ref/lib/stats"
	"v.io/x/ref/runtime/internal/flow/manager"
	"v.io/x/ref/runtime/internal/lib/publisher"
	inaming "v.io/x/ref/runtime/internal/naming"
)

// TODO(mattr): add/removeAddresses

type xserver struct {
	sync.Mutex
	// stop is kept for backward compatibilty to implement Stop().
	// TODO(mattr): deprecate Stop.
	stop func()
	// ctx is used by the server to make internal RPCs, error messages etc.
	ctx               *context.T
	cancel            context.CancelFunc // function to cancel the above context.
	flowMgr           flow.Manager
	publisher         publisher.Publisher // publisher to publish mounttable mounts.
	settingsPublisher *pubsub.Publisher   // pubsub publisher for dhcp
	valid             chan struct{}
	blessings         security.Blessings
	typeCache         *typeCache
	state             rpc.ServerState // the current state of the server.

	endpoints map[string]*inaming.Endpoint // endpoints that the server is listening on.
	lnErrors  []error                      // errors from listening

	disp               rpc.Dispatcher // dispatcher to serve RPCs
	dispReserved       rpc.Dispatcher // dispatcher for reserved methods
	active             sync.WaitGroup // active goroutines we've spawned.
	preferredProtocols []string       // protocols to use when resolving proxy name to endpoint.
	servesMountTable   bool
	isLeaf             bool

	stats *rpcStats // stats for this server.
}

func WithNewServer(ctx *context.T,
	name string, object interface{}, authorizer security.Authorizer,
	settingsPublisher *pubsub.Publisher,
	opts ...rpc.ServerOpt) (*context.T, rpc.Server, error) {
	if object == nil {
		return ctx, nil, verror.New(verror.ErrBadArg, ctx, "nil object")
	}
	invoker, err := objectToInvoker(object)
	if err != nil {
		return ctx, nil, verror.New(verror.ErrBadArg, ctx, fmt.Sprintf("bad object: %v", err))
	}
	d := &leafDispatcher{invoker, authorizer}
	opts = append([]rpc.ServerOpt{options.IsLeaf(true)}, opts...)
	return WithNewDispatchingServer(ctx, name, d, settingsPublisher, opts...)
}

func WithNewDispatchingServer(ctx *context.T,
	name string, dispatcher rpc.Dispatcher,
	settingsPublisher *pubsub.Publisher,
	opts ...rpc.ServerOpt) (*context.T, rpc.Server, error) {
	if dispatcher == nil {
		return ctx, nil, verror.New(verror.ErrBadArg, ctx, "nil dispatcher")
	}

	rid, err := naming.NewRoutingID()
	if err != nil {
		return ctx, nil, err
	}

	ctx, stop := context.WithCancel(ctx)
	rootCtx, cancel := context.WithRootCancel(ctx)
	statsPrefix := naming.Join("rpc", "server", "routing-id", rid.String())
	s := &xserver{
		stop:              stop,
		cancel:            cancel,
		blessings:         v23.GetPrincipal(rootCtx).BlessingStore().Default(),
		stats:             newRPCStats(statsPrefix),
		settingsPublisher: settingsPublisher,
		valid:             make(chan struct{}),
		disp:              dispatcher,
		typeCache:         newTypeCache(),
		state:             rpc.ServerActive,
		endpoints:         make(map[string]*inaming.Endpoint),
	}
	for _, opt := range opts {
		switch opt := opt.(type) {
		case options.ServesMountTable:
			s.servesMountTable = bool(opt)
		case options.IsLeaf:
			s.isLeaf = bool(opt)
		case ReservedNameDispatcher:
			s.dispReserved = opt.Dispatcher
		case PreferredServerResolveProtocols:
			s.preferredProtocols = []string(opt)
		case options.ServerBlessings:
			s.blessings = opt.Blessings
			if !reflect.DeepEqual(s.blessings.PublicKey(), v23.GetPrincipal(rootCtx).PublicKey()) {
				cancel()
				return ctx, nil, verror.New(verror.ErrBadArg, ctx,
					newErrServerBlessingsWrongPublicKey(ctx))
			}
		}
	}

	s.flowMgr = manager.NewWithBlessings(rootCtx, s.blessings, rid, settingsPublisher)
	rootCtx, _, err = v23.WithNewClient(rootCtx,
		clientFlowManagerOpt{s.flowMgr},
		PreferredProtocols(s.preferredProtocols))
	if err != nil {
		cancel()
		return ctx, nil, err
	}
	s.ctx = rootCtx
	s.publisher = publisher.New(rootCtx, v23.GetNamespace(rootCtx), publishPeriod)

	// TODO(caprita): revist printing the blessings with string, and
	// instead expose them as a list.
	blessingsStatsName := naming.Join(statsPrefix, "security", "blessings")
	stats.NewString(blessingsStatsName).Set(s.blessings.String())

	s.listen(rootCtx, v23.GetListenSpec(rootCtx))
	if len(name) > 0 {
		// TODO(mattr): We only call AddServer here, but if someone calls AddName
		// later there will be no servers?
		s.Lock()
		for k, _ := range s.endpoints {
			s.publisher.AddServer(k)
		}
		s.Unlock()
		s.publisher.AddName(name, s.servesMountTable, s.isLeaf)
		vtrace.GetSpan(s.ctx).Annotate("Serving under name: " + name)
	}

	go func() {
		<-ctx.Done()
		s.Lock()
		s.state = rpc.ServerStopping
		s.Unlock()
		serverDebug := fmt.Sprintf("Dispatcher: %T, Status:[%v]", s.disp, s.Status())
		s.ctx.VI(1).Infof("Stop: %s", serverDebug)
		defer s.ctx.VI(1).Infof("Stop done: %s", serverDebug)

		s.stats.stop()
		s.publisher.Stop()

		done := make(chan struct{})
		go func() {
			s.flowMgr.StopListening(ctx)
			s.publisher.WaitForStop()
			// At this point no new flows should arrive.  Wait for existing calls
			// to complete.
			s.active.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second): // TODO(mattr): This should be configurable.
			s.ctx.Errorf("%s: Timed out waiting for active requests to complete", serverDebug)
		}
		// Now we cancel the root context which closes all the connections
		// in the flow manager and cancels all the contexts used by
		// ongoing requests.  Hopefully this will bring all outstanding
		// operations to a close.
		s.cancel()
		<-s.flowMgr.Closed()
		close(s.valid)
		s.valid = nil
		// Now we really will wait forever.  If this doesn't exit, there's something
		// wrong with the users code.
		<-done
		s.Lock()
		s.state = rpc.ServerStopped
		s.Unlock()
	}()
	return rootCtx, s, nil
}

func (s *xserver) Status() rpc.ServerStatus {
	status := rpc.ServerStatus{}
	status.ServesMountTable = s.servesMountTable
	status.Mounts = s.publisher.Status()
	s.Lock()
	status.Valid = s.valid
	status.State = s.state
	for _, e := range s.endpoints {
		status.Endpoints = append(status.Endpoints, e)
	}
	status.Errors = s.lnErrors
	s.Unlock()
	return status
}

// resolveToEndpoint resolves an object name or address to an endpoint.
func (s *xserver) resolveToEndpoint(ctx *context.T, address string) (naming.Endpoint, error) {
	var resolved *naming.MountEntry
	var err error
	// TODO(mattr): Why should ns be nil?
	if ns := v23.GetNamespace(ctx); ns != nil {
		if resolved, err = ns.Resolve(ctx, address); err != nil {
			return nil, err
		}
	} else {
		// Fake a namespace resolution
		resolved = &naming.MountEntry{Servers: []naming.MountedServer{
			{Server: address},
		}}
	}
	// An empty set of protocols means all protocols...
	if resolved.Servers, err = filterAndOrderServers(resolved.Servers, s.preferredProtocols); err != nil {
		return nil, err
	}
	for _, n := range resolved.Names() {
		address, suffix := naming.SplitAddressName(n)
		if suffix != "" {
			continue
		}
		if ep, err := inaming.NewEndpoint(address); err == nil {
			return ep, nil
		}
	}
	return nil, verror.New(errFailedToResolveToEndpoint, s.ctx, address)
}

// createEndpoint adds server publishing information to the ep from the manager.
func (s *xserver) createEndpoint(lep naming.Endpoint) *inaming.Endpoint {
	n := *(lep.(*inaming.Endpoint))
	n.IsMountTable = s.servesMountTable
	n.IsLeaf = s.isLeaf
	return &n
}

func (s *xserver) listen(ctx *context.T, listenSpec rpc.ListenSpec) {
	defer s.Unlock()
	s.Lock()
	if len(listenSpec.Proxy) > 0 {
		ep, err := s.resolveToEndpoint(ctx, listenSpec.Proxy)
		if err != nil {
			s.ctx.VI(2).Infof("resolveToEndpoint(%q) failed: %v", listenSpec.Proxy, err)
		} else {
			err = s.flowMgr.ProxyListen(ctx, ep)
			if err != nil {
				s.ctx.VI(2).Infof("Listen(%q, %q, ...) failed: %v", inaming.Network, ep, err)
			}
		}
		if err != nil {
			s.lnErrors = append(s.lnErrors, err)
		}
	}
	for _, addr := range listenSpec.Addrs {
		if len(addr.Address) > 0 {
			err := s.flowMgr.Listen(ctx, addr.Protocol, addr.Address)
			if err != nil {
				s.ctx.VI(2).Infof("Listen(%q, %q, ...) failed: %v", addr.Protocol, addr.Address, err)
				s.lnErrors = append(s.lnErrors, err)
			}
		}
	}

	// We call updateEndpointsLocked in serial once to populate our endpoints for
	// server status with at least one endpoint.
	leps, changed := s.flowMgr.ListeningEndpoints()
	s.updateEndpointsLocked(leps)
	s.active.Add(2)
	go s.updateEndpointsLoop(changed)
	go s.acceptLoop(ctx)
}

func (s *xserver) updateEndpointsLoop(changed <-chan struct{}) {
	defer s.active.Done()
	var leps []naming.Endpoint
	for changed != nil {
		<-changed
		leps, changed = s.flowMgr.ListeningEndpoints()
		s.Lock()
		s.updateEndpointsLocked(leps)
		s.Unlock()
	}
}

func (s *xserver) updateEndpointsLocked(leps []naming.Endpoint) {
	endpoints := make(map[string]*inaming.Endpoint)
	for _, ep := range leps {
		sep := s.createEndpoint(ep)
		endpoints[sep.String()] = sep
	}
	// Endpoints to add and remaove.
	rmEps := setDiff(s.endpoints, endpoints)
	addEps := setDiff(endpoints, s.endpoints)
	for k := range rmEps {
		delete(s.endpoints, k)
	}
	for k, ep := range addEps {
		s.endpoints[k] = ep
	}
	if s.valid != nil {
		close(s.valid)
		s.valid = make(chan struct{})
	}

	s.Unlock()
	for k := range rmEps {
		s.publisher.RemoveServer(k)
	}
	for k := range addEps {
		s.publisher.AddServer(k)
	}
	s.Lock()
}

// setDiff returns the endpoints in a that are not in b.
func setDiff(a, b map[string]*inaming.Endpoint) map[string]*inaming.Endpoint {
	ret := make(map[string]*inaming.Endpoint)
	for k, ep := range a {
		if _, ok := b[k]; !ok {
			ret[k] = ep
		}
	}
	return ret
}

func (s *xserver) acceptLoop(ctx *context.T) error {
	var calls sync.WaitGroup
	defer func() {
		calls.Wait()
		s.active.Done()
		s.ctx.VI(1).Infof("rpc: Stopped accepting")
	}()
	for {
		// TODO(mattr): We need to interrupt Accept at some point.
		// Should we interrupt it by canceling the context?
		fl, err := s.flowMgr.Accept(ctx)
		if err != nil {
			s.ctx.VI(10).Infof("rpc: Accept failed: %v", err)
			return err
		}
		calls.Add(1)
		go func(fl flow.Flow) {
			defer calls.Done()
			var ty [1]byte
			if _, err := io.ReadFull(fl, ty[:]); err != nil {
				s.ctx.VI(1).Infof("failed to read flow type: %v", err)
				return
			}
			switch ty[0] {
			case dataFlow:
				fs, err := newXFlowServer(fl, s)
				if err != nil {
					s.ctx.VI(1).Infof("newFlowServer failed %v", err)
					return
				}
				if err := fs.serve(); err != nil {
					// TODO(caprita): Logging errors here is too spammy. For example, "not
					// authorized" errors shouldn't be logged as server errors.
					// TODO(cnicolaou): revisit this when verror2 transition is
					// done.
					if err != io.EOF {
						s.ctx.VI(2).Infof("Flow.serve failed: %v", err)
					}
				}
			case typeFlow:
				if write := s.typeCache.writer(fl.Conn()); write != nil {
					write(fl, nil)
				}
			}
		}(fl)
	}
}

func (s *xserver) AddName(name string) error {
	defer apilog.LogCallf(nil, "name=%.10s...", name)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	if len(name) == 0 {
		return verror.New(verror.ErrBadArg, s.ctx, "name is empty")
	}
	s.Lock()
	defer s.Unlock()
	vtrace.GetSpan(s.ctx).Annotate("Serving under name: " + name)
	s.publisher.AddName(name, s.servesMountTable, s.isLeaf)
	return nil
}

func (s *xserver) RemoveName(name string) {
	defer apilog.LogCallf(nil, "name=%.10s...", name)(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	s.Lock()
	defer s.Unlock()
	vtrace.GetSpan(s.ctx).Annotate("Removed name: " + name)
	s.publisher.RemoveName(name)
}

func (s *xserver) Stop() error {
	defer apilog.LogCall(nil)(nil) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	s.stop()
	<-s.Closed()
	return nil
}

func (s *xserver) Closed() <-chan struct{} {
	return s.ctx.Done()
}

// flowServer implements the RPC server-side protocol for a single RPC, over a
// flow that's already connected to the client.
type xflowServer struct {
	server *xserver       // rpc.Server that this flow server belongs to
	disp   rpc.Dispatcher // rpc.Dispatcher that will serve RPCs on this flow
	flow   flow.Flow      // underlying flow

	// Fields filled in during the server invocation.
	dec              *vom.Decoder // to decode requests and args from the client
	enc              *vom.Encoder // to encode responses and results to the client
	grantedBlessings security.Blessings
	method, suffix   string
	tags             []*vdl.Value
	discharges       map[string]security.Discharge
	starttime        time.Time
	endStreamArgs    bool // are the stream args at EOF?
}

var (
	_ rpc.StreamServerCall = (*xflowServer)(nil)
	_ security.Call        = (*xflowServer)(nil)
)

func newXFlowServer(flow flow.Flow, server *xserver) (*xflowServer, error) {
	fs := &xflowServer{
		server:     server,
		disp:       server.disp,
		flow:       flow,
		discharges: make(map[string]security.Discharge),
	}
	return fs, nil
}

// authorizeVtrace works by simulating a call to __debug/vtrace.Trace.  That
// rpc is essentially equivalent in power to the data we are attempting to
// attach here.
func (fs *xflowServer) authorizeVtrace(ctx *context.T) error {
	// Set up a context as though we were calling __debug/vtrace.
	params := &security.CallParams{}
	params.Copy(fs)
	params.Method = "Trace"
	params.MethodTags = []*vdl.Value{vdl.ValueOf(access.Debug)}
	params.Suffix = "__debug/vtrace"

	var auth security.Authorizer
	if fs.server.dispReserved != nil {
		_, auth, _ = fs.server.dispReserved.Lookup(ctx, params.Suffix)
	}
	return authorize(ctx, security.NewCall(params), auth)
}

func (fs *xflowServer) serve() error {
	defer fs.flow.Close()

	ctx, results, err := fs.processRequest()
	vtrace.GetSpan(ctx).Finish()

	var traceResponse vtrace.Response
	// Check if the caller is permitted to view vtrace data.
	if fs.authorizeVtrace(ctx) == nil {
		traceResponse = vtrace.GetResponse(ctx)
	}

	if err != nil && fs.enc == nil {
		return err
	}

	// Respond to the client with the response header and positional results.
	response := rpc.Response{
		Error:            err,
		EndStreamResults: true,
		NumPosResults:    uint64(len(results)),
		TraceResponse:    traceResponse,
	}
	if err := fs.enc.Encode(response); err != nil {
		if err == io.EOF {
			return err
		}
		return verror.New(errResponseEncoding, ctx, fs.LocalEndpoint().String(), fs.RemoteEndpoint().String(), err)
	}
	if response.Error != nil {
		return response.Error
	}
	for ix, res := range results {
		if err := fs.enc.Encode(res); err != nil {
			if err == io.EOF {
				return err
			}
			return verror.New(errResultEncoding, ctx, ix, fmt.Sprintf("%T=%v", res, res), err)
		}
	}
	// TODO(ashankar): Should unread data from the flow be drained?
	//
	// Reason to do so:
	// The common stream.Flow implementation (v.io/x/ref/runtime/internal/rpc/stream/vc/reader.go)
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

func (fs *xflowServer) readRPCRequest(ctx *context.T) (*rpc.Request, error) {
	// Decode the initial request.
	var req rpc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return nil, verror.New(verror.ErrBadProtocol, ctx, newErrBadRequest(ctx, err))
	}
	return &req, nil
}

func (fs *xflowServer) processRequest() (*context.T, []interface{}, error) {
	fs.starttime = time.Now()

	// Set an initial deadline on the flow to ensure that we don't wait forever
	// for the initial read.
	ctx := fs.flow.SetDeadlineContext(fs.server.ctx, time.Now().Add(defaultCallTimeout))

	typeEnc, typeDec, err := fs.server.typeCache.get(ctx, fs.flow.Conn())
	if err != nil {
		return ctx, nil, err
	}
	fs.enc = vom.NewEncoderWithTypeEncoder(fs.flow, typeEnc)
	fs.dec = vom.NewDecoderWithTypeDecoder(fs.flow, typeDec)

	req, err := fs.readRPCRequest(ctx)
	if err != nil {
		// We don't know what the rpc call was supposed to be, but we'll create
		// a placeholder span so we can capture annotations.
		// TODO(mattr): I'm not sure this makes sense anymore, but I'll revisit it
		// when I'm doing another round of vtrace next quarter.
		ctx, _ = vtrace.WithNewSpan(ctx, fmt.Sprintf("\"%s\".UNKNOWN", fs.suffix))
		return ctx, nil, err
	}

	// Start building up a new context for the request now that we know
	// the header information.
	ctx = fs.server.ctx

	// We must call fs.drainDecoderArgs for any error that occurs
	// after this point, and before we actually decode the arguments.
	fs.method = req.Method
	fs.suffix = strings.TrimLeft(req.Suffix, "/")
	if req.Language != "" {
		ctx = i18n.WithLangID(ctx, i18n.LangID(req.Language))
	}

	// TODO(mattr): Currently this allows users to trigger trace collection
	// on the server even if they will not be allowed to collect the
	// results later.  This might be considered a DOS vector.
	spanName := fmt.Sprintf("\"%s\".%s", fs.suffix, fs.method)
	ctx, _ = vtrace.WithContinuedTrace(ctx, spanName, req.TraceRequest)
	ctx = fs.flow.SetDeadlineContext(ctx, req.Deadline.Time)

	if err := fs.readGrantedBlessings(ctx, req); err != nil {
		fs.drainDecoderArgs(int(req.NumPosArgs))
		return ctx, nil, err
	}
	// Lookup the invoker.
	invoker, auth, err := fs.lookup(ctx, fs.suffix, fs.method)
	if err != nil {
		fs.drainDecoderArgs(int(req.NumPosArgs))
		return ctx, nil, err
	}

	// Note that we strip the reserved prefix when calling the invoker so
	// that __Glob will call Glob.  Note that we've already assigned a
	// special invoker so that we never call the wrong method by mistake.
	strippedMethod := naming.StripReserved(fs.method)

	// Prepare invoker and decode args.
	numArgs := int(req.NumPosArgs)
	argptrs, tags, err := invoker.Prepare(ctx, strippedMethod, numArgs)
	fs.tags = tags
	if err != nil {
		fs.drainDecoderArgs(numArgs)
		return ctx, nil, err
	}
	if called, want := req.NumPosArgs, uint64(len(argptrs)); called != want {
		fs.drainDecoderArgs(numArgs)
		return ctx, nil, newErrBadNumInputArgs(ctx, fs.suffix, fs.method, called, want)
	}
	for ix, argptr := range argptrs {
		if err := fs.dec.Decode(argptr); err != nil {
			return ctx, nil, newErrBadInputArg(ctx, fs.suffix, fs.method, uint64(ix), err)
		}
	}

	// Check application's authorization policy.
	if err := authorize(ctx, fs, auth); err != nil {
		return ctx, nil, err
	}

	// Invoke the method.
	results, err := invoker.Invoke(ctx, fs, strippedMethod, argptrs)
	fs.server.stats.record(fs.method, time.Since(fs.starttime))
	return ctx, results, err
}

// drainDecoderArgs drains the next n arguments encoded onto the flows decoder.
// This is needed to ensure that the client is able to encode all of its args
// before the server closes its flow. This guarantees that the client will
// consistently get the server's error response.
// TODO(suharshs): Figure out a better way to solve this race condition without
// unnecessarily reading all arguments.
func (fs *xflowServer) drainDecoderArgs(n int) error {
	for i := 0; i < n; i++ {
		if err := fs.dec.Ignore(); err != nil {
			return err
		}
	}
	return nil
}

// lookup returns the invoker and authorizer responsible for serving the given
// name and method.  The suffix is stripped of any leading slashes. If it begins
// with rpc.DebugKeyword, we use the internal debug dispatcher to look up the
// invoker. Otherwise, and we use the server's dispatcher. The suffix and method
// value may be modified to match the actual suffix and method to use.
func (fs *xflowServer) lookup(ctx *context.T, suffix string, method string) (rpc.Invoker, security.Authorizer, error) {
	if naming.IsReserved(method) {
		return reservedInvoker(fs.disp, fs.server.dispReserved), security.AllowEveryone(), nil
	}
	disp := fs.disp
	if naming.IsReserved(suffix) {
		disp = fs.server.dispReserved
	} else if fs.server.isLeaf && suffix != "" {
		innerErr := verror.New(errUnexpectedSuffix, ctx, suffix)
		return nil, nil, verror.New(verror.ErrUnknownSuffix, ctx, suffix, innerErr)
	}
	if disp != nil {
		obj, auth, err := disp.Lookup(ctx, suffix)
		switch {
		case err != nil:
			return nil, nil, err
		case obj != nil:
			invoker, err := objectToInvoker(obj)
			if err != nil {
				return nil, nil, verror.New(verror.ErrInternal, ctx, "invalid received object", err)
			}
			return invoker, auth, nil
		}
	}
	return nil, nil, verror.New(verror.ErrUnknownSuffix, ctx, suffix)
}

func (fs *xflowServer) readGrantedBlessings(ctx *context.T, req *rpc.Request) error {
	if req.GrantedBlessings.IsZero() {
		return nil
	}
	// If additional credentials are provided, make them available in the context
	// Detect unusable blessings now, rather then discovering they are unusable on
	// first use.
	//
	// TODO(ashankar,ataly): Potential confused deputy attack: The client provides
	// the server's identity as the blessing. Figure out what we want to do about
	// this - should servers be able to assume that a blessing is something that
	// does not have the authorizations that the server's own identity has?
	if got, want := req.GrantedBlessings.PublicKey(), fs.LocalPrincipal().PublicKey(); got != nil && !reflect.DeepEqual(got, want) {
		return verror.New(verror.ErrNoAccess, ctx,
			verror.New(errBlessingsNotBound, ctx, got, want))
	}
	fs.grantedBlessings = req.GrantedBlessings
	return nil
}

// Send implements the rpc.Stream method.
func (fs *xflowServer) Send(item interface{}) error {
	defer apilog.LogCallf(nil, "item=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	// The empty response header indicates what follows is a streaming result.
	if err := fs.enc.Encode(rpc.Response{}); err != nil {
		return err
	}
	return fs.enc.Encode(item)
}

// Recv implements the rpc.Stream method.
func (fs *xflowServer) Recv(itemptr interface{}) error {
	defer apilog.LogCallf(nil, "itemptr=")(nil, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	var req rpc.Request
	if err := fs.dec.Decode(&req); err != nil {
		return err
	}
	if req.EndStreamArgs {
		fs.endStreamArgs = true
		return io.EOF
	}
	return fs.dec.Decode(itemptr)
}

// Implementations of rpc.ServerCall and security.Call methods.

func (fs *xflowServer) Security() security.Call {
	//nologcall
	return fs
}
func (fs *xflowServer) LocalDischarges() map[string]security.Discharge {
	//nologcall
	return fs.flow.LocalDischarges()
}
func (fs *xflowServer) RemoteDischarges() map[string]security.Discharge {
	//nologcall
	return fs.flow.RemoteDischarges()
}
func (fs *xflowServer) Server() rpc.Server {
	//nologcall
	return fs.server
}
func (fs *xflowServer) Timestamp() time.Time {
	//nologcall
	return fs.starttime
}
func (fs *xflowServer) Method() string {
	//nologcall
	return fs.method
}
func (fs *xflowServer) MethodTags() []*vdl.Value {
	//nologcall
	return fs.tags
}
func (fs *xflowServer) Suffix() string {
	//nologcall
	return fs.suffix
}
func (fs *xflowServer) LocalPrincipal() security.Principal {
	//nologcall
	return v23.GetPrincipal(fs.server.ctx)
}
func (fs *xflowServer) LocalBlessings() security.Blessings {
	//nologcall
	return fs.flow.LocalBlessings()
}
func (fs *xflowServer) RemoteBlessings() security.Blessings {
	//nologcall
	return fs.flow.RemoteBlessings()
}
func (fs *xflowServer) GrantedBlessings() security.Blessings {
	//nologcall
	return fs.grantedBlessings
}
func (fs *xflowServer) LocalEndpoint() naming.Endpoint {
	//nologcall
	return fs.flow.LocalEndpoint()
}
func (fs *xflowServer) RemoteEndpoint() naming.Endpoint {
	//nologcall
	return fs.flow.RemoteEndpoint()
}

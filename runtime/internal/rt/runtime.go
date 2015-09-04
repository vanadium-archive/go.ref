// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"v.io/x/lib/metadata"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/i18n"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vtrace"

	"v.io/x/ref/internal/logger"
	"v.io/x/ref/lib/apilog"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/pubsub"
	"v.io/x/ref/lib/stats"
	_ "v.io/x/ref/lib/stats/sysstats"
	"v.io/x/ref/runtime/internal/flow/manager"
	"v.io/x/ref/runtime/internal/lib/dependency"
	inaming "v.io/x/ref/runtime/internal/naming"
	inamespace "v.io/x/ref/runtime/internal/naming/namespace"
	irpc "v.io/x/ref/runtime/internal/rpc"
	"v.io/x/ref/runtime/internal/rpc/stream"
	imanager "v.io/x/ref/runtime/internal/rpc/stream/manager"
	ivtrace "v.io/x/ref/runtime/internal/vtrace"
)

const (
	none = iota
	xclients
	xservers
)

var transitionState = none

func init() {
	switch ts := os.Getenv("V23_RPC_TRANSITION_STATE"); ts {
	case "xclients":
		transitionState = xclients
	case "xservers":
		transitionState = xservers
	case "":
		transitionState = none
	default:
		panic("Unknown transition state: " + ts)
	}
}

type contextKey int

const (
	streamManagerKey = contextKey(iota)
	clientKey
	namespaceKey
	principalKey
	backgroundKey
	reservedNameKey
	listenKey
	flowManagerKey

	// initKey is used to store values that are only set at init time.
	initKey
)

type initData struct {
	appCycle          v23.AppCycle
	protocols         []string
	settingsPublisher *pubsub.Publisher
	settingsName      string
}

type vtraceDependency struct{}

// Runtime implements the v23.Runtime interface.
// Please see the interface definition for documentation of the
// individiual methods.
type Runtime struct {
	ctx  *context.T
	deps *dependency.Graph
}

func Init(
	ctx *context.T,
	appCycle v23.AppCycle,
	protocols []string,
	listenSpec *rpc.ListenSpec,
	settingsPublisher *pubsub.Publisher,
	settingsName string,
	flags flags.RuntimeFlags,
	reservedDispatcher rpc.Dispatcher) (*Runtime, *context.T, v23.Shutdown, error) {
	r := &Runtime{deps: dependency.NewGraph()}

	ctx = context.WithValue(ctx, initKey, &initData{
		protocols:         protocols,
		appCycle:          appCycle,
		settingsPublisher: settingsPublisher,
		settingsName:      settingsName,
	})

	if listenSpec != nil {
		ctx = context.WithValue(ctx, listenKey, listenSpec.Copy())
	}

	if reservedDispatcher != nil {
		ctx = context.WithValue(ctx, reservedNameKey, reservedDispatcher)
	}

	// Configure the context to use the global logger.
	ctx = context.WithLogger(ctx, logger.Global())

	// We want to print out metadata only into the log files, to avoid
	// spamming stderr, see #1246.
	//
	// TODO(caprita): We should add it to the log file header information;
	// since that requires changes to the llog and vlog packages, for now we
	// condition printing of metadata on having specified an explicit
	// log_dir for the program.  It's a hack, but it gets us the metadata
	// to device manager-run apps and avoids it for command-lines, which is
	// a good enough approximation.
	if logger.Manager(ctx).LogDir() != os.TempDir() {
		ctx.Infof(metadata.ToXML())
	}

	// Setup the initial trace.
	ctx, err := ivtrace.Init(ctx, flags.Vtrace)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, _ = vtrace.WithNewTrace(ctx)
	r.addChild(ctx, vtraceDependency{}, func() {
		vtrace.FormatTraces(os.Stderr, vtrace.GetStore(ctx).TraceRecords(), nil)
	})

	// Setup i18n.
	ctx = i18n.WithLangID(ctx, i18n.LangIDFromEnv())
	if len(flags.I18nCatalogue) != 0 {
		cat := i18n.Cat()
		for _, filename := range strings.Split(flags.I18nCatalogue, ",") {
			err := cat.MergeFromFile(filename)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: i18n: error reading i18n catalogue file %q: %s\n", os.Args[0], filename, err)
			}
		}
	}

	// Setup the program name.
	ctx = verror.WithComponentName(ctx, filepath.Base(os.Args[0]))

	// Enable signal handling.
	r.initSignalHandling(ctx)

	// Set the initial namespace.
	ctx, _, err = r.setNewNamespace(ctx, flags.NamespaceRoots...)
	if err != nil {
		return nil, nil, nil, err
	}

	// Create and set the principal
	principal, deps, shutdown, err := r.initPrincipal(ctx, flags.Credentials)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, err = r.setPrincipal(ctx, principal, shutdown, deps...)
	if err != nil {
		return nil, nil, nil, err
	}

	// Setup authenticated "networking"
	ctx, err = r.WithNewStreamManager(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	// Add the flow.Manager to the context.
	ctx, _, err = r.ExperimentalWithNewFlowManager(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	r.ctx = ctx
	return r, r.WithBackgroundContext(ctx), r.shutdown, nil
}

func (r *Runtime) addChild(ctx *context.T, me interface{}, stop func(), dependsOn ...interface{}) error {
	if err := r.deps.Depend(me, dependsOn...); err != nil {
		stop()
		return err
	} else if done := ctx.Done(); done != nil {
		go func() {
			<-done
			finish := r.deps.CloseAndWait(me)
			stop()
			finish()
		}()
	}
	return nil
}

func (r *Runtime) Init(ctx *context.T) error {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return r.initMgmt(ctx)
}

func (r *Runtime) shutdown() {
	r.deps.CloseAndWaitForAll()
	r.ctx.FlushLog()
}

func (r *Runtime) initSignalHandling(ctx *context.T) {
	// TODO(caprita): Given that our device manager implementation is to
	// kill all child apps when the device manager dies, we should
	// enable SIGHUP on apps by default.

	// Automatically handle SIGHUP to prevent applications started as
	// daemons from being killed.  The developer can choose to still listen
	// on SIGHUP and take a different action if desired.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)
	go func() {
		for {
			sig, ok := <-signals
			if !ok {
				break
			}
			r.ctx.Infof("Received signal %v", sig)
		}
	}()
	r.addChild(ctx, signals, func() {
		signal.Stop(signals)
		close(signals)
	})
}

func (*Runtime) NewEndpoint(ep string) (naming.Endpoint, error) {
	// nologcall
	return inaming.NewEndpoint(ep)
}

func (r *Runtime) NewServer(ctx *context.T, opts ...rpc.ServerOpt) (rpc.DeprecatedServer, error) {
	defer apilog.LogCallf(ctx, "opts...=%v", opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	// Create a new RoutingID (and StreamManager) for each server.
	sm, err := newStreamManager(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create rpc/stream/Manager: %v", err)
	}

	ns, _ := ctx.Value(namespaceKey).(namespace.T)
	principal, _ := ctx.Value(principalKey).(security.Principal)
	client, _ := ctx.Value(clientKey).(rpc.Client)

	otherOpts := append([]rpc.ServerOpt{}, opts...)

	if reservedDispatcher := r.GetReservedNameDispatcher(ctx); reservedDispatcher != nil {
		otherOpts = append(otherOpts, irpc.ReservedNameDispatcher{
			Dispatcher: reservedDispatcher,
		})
	}

	id, _ := ctx.Value(initKey).(*initData)
	if id.protocols != nil {
		otherOpts = append(otherOpts, irpc.PreferredServerResolveProtocols(id.protocols))
	}
	if !hasServerBlessingsOpt(opts) && principal != nil {
		otherOpts = append(otherOpts, options.ServerBlessings{
			Blessings: principal.BlessingStore().Default(),
		})
	}
	server, err := irpc.InternalNewServer(ctx, sm, ns, id.settingsPublisher, id.settingsName, r.GetClient(ctx), otherOpts...)
	if err != nil {
		return nil, err
	}
	stop := func() {
		if err := server.Stop(); err != nil {
			r.ctx.Errorf("A server could not be stopped: %v", err)
		}
		sm.Shutdown()
	}
	deps := []interface{}{client, vtraceDependency{}}
	if principal != nil {
		deps = append(deps, principal)
	}
	if err = r.addChild(ctx, server, stop, deps...); err != nil {
		return nil, err
	}
	return server, nil
}

func hasServerBlessingsOpt(opts []rpc.ServerOpt) bool {
	for _, o := range opts {
		if _, ok := o.(options.ServerBlessings); ok {
			return true
		}
	}
	return false
}

func newStreamManager(ctx *context.T) (stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	sm := imanager.InternalNew(ctx, rid)
	return sm, nil
}

func newFlowManager(ctx *context.T) (flow.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	return manager.New(ctx, rid), nil
}

func (r *Runtime) setNewFlowManager(ctx *context.T) (*context.T, flow.Manager, error) {
	fm, err := newFlowManager(ctx)
	if err != nil {
		return nil, nil, err
	}
	// TODO(mattr): How can we close a flow manager.
	if err = r.addChild(ctx, fm, func() {}); err != nil {
		return ctx, nil, err
	}
	newctx := context.WithValue(ctx, flowManagerKey, fm)
	return newctx, fm, nil
}

func (r *Runtime) setNewStreamManager(ctx *context.T) (*context.T, error) {
	sm, err := newStreamManager(ctx)
	if err != nil {
		return nil, err
	}
	newctx := context.WithValue(ctx, streamManagerKey, sm)
	if err = r.addChild(ctx, sm, sm.Shutdown); err != nil {
		return ctx, err
	}
	return newctx, err
}

func (r *Runtime) WithNewStreamManager(ctx *context.T) (*context.T, error) {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	newctx, err := r.setNewStreamManager(ctx)
	if err != nil {
		return ctx, err
	}

	// Create a new client since it depends on the stream manager.
	newctx, _, err = r.WithNewClient(newctx)
	if err != nil {
		return ctx, err
	}
	return newctx, nil
}

func (r *Runtime) setPrincipal(ctx *context.T, principal security.Principal, shutdown func(), deps ...interface{}) (*context.T, error) {
	if principal != nil {
		// We uniquely identify a principal with "security/principal/<publicKey>"
		principalName := "security/principal/" + principal.PublicKey().String()
		stats.NewStringFunc(principalName+"/blessingstore", principal.BlessingStore().DebugString)
		stats.NewStringFunc(principalName+"/blessingroots", principal.Roots().DebugString)
	}
	ctx = context.WithValue(ctx, principalKey, principal)
	return ctx, r.addChild(ctx, principal, shutdown, deps...)
}

func (r *Runtime) WithPrincipal(ctx *context.T, principal security.Principal) (*context.T, error) {
	defer apilog.LogCallf(ctx, "principal=%v", principal)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	var err error
	newctx := ctx

	// TODO(mattr, suharshs): If there user gives us some principal that has dependencies
	// we don't know about, we will not honour those dependencies during shutdown.
	// For example if they create an agent principal with some client, we don't know
	// about that, so servers based of this new principal will not prevent the client
	// from terminating early.
	if newctx, err = r.setPrincipal(ctx, principal, func() {}); err != nil {
		return ctx, err
	}
	if newctx, err = r.setNewStreamManager(newctx); err != nil {
		return ctx, err
	}
	if newctx, _, err = r.setNewFlowManager(newctx); err != nil {
		return ctx, err
	}
	if newctx, _, err = r.setNewNamespace(newctx, r.GetNamespace(ctx).Roots()...); err != nil {
		return ctx, err
	}
	if newctx, _, err = r.WithNewClient(newctx); err != nil {
		return ctx, err
	}

	return newctx, nil
}

func (*Runtime) GetPrincipal(ctx *context.T) security.Principal {
	// nologcall
	p, _ := ctx.Value(principalKey).(security.Principal)
	return p
}

func (r *Runtime) WithNewClient(ctx *context.T, opts ...rpc.ClientOpt) (*context.T, rpc.Client, error) {
	defer apilog.LogCallf(ctx, "opts...=%v", opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	otherOpts := append([]rpc.ClientOpt{}, opts...)

	p, _ := ctx.Value(principalKey).(security.Principal)
	sm, _ := ctx.Value(streamManagerKey).(stream.Manager)
	ns, _ := ctx.Value(namespaceKey).(namespace.T)
	fm, _ := ctx.Value(flowManagerKey).(flow.Manager)
	otherOpts = append(otherOpts, imanager.DialTimeout(5*time.Minute))

	if id, _ := ctx.Value(initKey).(*initData); id.protocols != nil {
		otherOpts = append(otherOpts, irpc.PreferredProtocols(id.protocols))
	}
	var client rpc.Client
	var err error
	deps := []interface{}{vtraceDependency{}}

	if fm != nil && transitionState >= xclients {
		client, err = irpc.NewTransitionClient(ctx, sm, ns, otherOpts...)
		deps = append(deps, fm, sm)
	} else {
		client, err = irpc.InternalNewClient(sm, ns, otherOpts...)
		deps = append(deps, sm)
	}

	if err != nil {
		return ctx, nil, err
	}
	newctx := context.WithValue(ctx, clientKey, client)
	if p != nil {
		deps = append(deps, p)
	}
	if err = r.addChild(ctx, client, client.Close, deps...); err != nil {
		return ctx, nil, err
	}
	return newctx, client, err
}

func (*Runtime) GetClient(ctx *context.T) rpc.Client {
	// nologcall
	cl, _ := ctx.Value(clientKey).(rpc.Client)
	return cl
}

func (r *Runtime) setNewNamespace(ctx *context.T, roots ...string) (*context.T, namespace.T, error) {
	ns, err := inamespace.New(roots...)
	if err != nil {
		return nil, nil, err
	}

	if oldNS := r.GetNamespace(ctx); oldNS != nil {
		ns.CacheCtl(oldNS.CacheCtl()...)
	}

	if err == nil {
		ctx = context.WithValue(ctx, namespaceKey, ns)
	}
	return ctx, ns, err
}

func (r *Runtime) WithNewNamespace(ctx *context.T, roots ...string) (*context.T, namespace.T, error) {
	defer apilog.LogCallf(ctx, "roots...=%v", roots)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	newctx, ns, err := r.setNewNamespace(ctx, roots...)
	if err != nil {
		return ctx, nil, err
	}

	// Replace the client since it depends on the namespace.
	newctx, _, err = r.WithNewClient(newctx)
	if err != nil {
		return ctx, nil, err
	}

	return newctx, ns, err
}

func (*Runtime) GetNamespace(ctx *context.T) namespace.T {
	// nologcall
	ns, _ := ctx.Value(namespaceKey).(namespace.T)
	return ns
}

func (*Runtime) GetAppCycle(ctx *context.T) v23.AppCycle {
	// nologcall
	id, _ := ctx.Value(initKey).(*initData)
	return id.appCycle
}

func (*Runtime) GetListenSpec(ctx *context.T) rpc.ListenSpec {
	// nologcall
	ls, _ := ctx.Value(listenKey).(rpc.ListenSpec)
	return ls
}

func (*Runtime) WithListenSpec(ctx *context.T, ls rpc.ListenSpec) *context.T {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return context.WithValue(ctx, listenKey, ls.Copy())
}

func (*Runtime) WithBackgroundContext(ctx *context.T) *context.T {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	// Note we add an extra context with a nil value here.
	// This prevents users from travelling back through the
	// chain of background contexts.
	ctx = context.WithValue(ctx, backgroundKey, nil)
	return context.WithValue(ctx, backgroundKey, ctx)
}

func (*Runtime) GetBackgroundContext(ctx *context.T) *context.T {
	// nologcall
	bctx, _ := ctx.Value(backgroundKey).(*context.T)
	if bctx == nil {
		// There should always be a background context.  If we don't find
		// it, that means that the user passed us the background context
		// in hopes of following the chain.  Instead we just give them
		// back what they sent in, which is correct.
		return ctx
	}
	return bctx
}

func (*Runtime) WithReservedNameDispatcher(ctx *context.T, d rpc.Dispatcher) *context.T {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	return context.WithValue(ctx, reservedNameKey, d)
}

func (*Runtime) GetReservedNameDispatcher(ctx *context.T) rpc.Dispatcher {
	// nologcall
	if d, ok := ctx.Value(reservedNameKey).(rpc.Dispatcher); ok {
		return d
	}
	return nil
}

func (*Runtime) ExperimentalGetFlowManager(ctx *context.T) flow.Manager {
	// nologcall
	if d, ok := ctx.Value(flowManagerKey).(flow.Manager); ok {
		return d
	}
	return nil
}

func (r *Runtime) ExperimentalWithNewFlowManager(ctx *context.T) (*context.T, flow.Manager, error) {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	newctx, m, err := r.setNewFlowManager(ctx)
	if err != nil {
		return ctx, nil, err
	}
	// Create a new client since it depends on the flow manager.
	newctx, _, err = r.WithNewClient(newctx)
	if err != nil {
		return ctx, nil, err
	}
	return newctx, m, nil
}

func (r *Runtime) commonServerInit(ctx *context.T, opts ...rpc.ServerOpt) (*context.T, *pubsub.Publisher, string, []rpc.ServerOpt, error) {
	newctx, _, err := r.ExperimentalWithNewFlowManager(ctx)
	if err != nil {
		return ctx, nil, "", nil, err
	}
	otherOpts := append([]rpc.ServerOpt{}, opts...)
	if reservedDispatcher := r.GetReservedNameDispatcher(ctx); reservedDispatcher != nil {
		otherOpts = append(otherOpts, irpc.ReservedNameDispatcher{
			Dispatcher: reservedDispatcher,
		})
	}
	id, _ := ctx.Value(initKey).(*initData)
	if id.protocols != nil {
		otherOpts = append(otherOpts, irpc.PreferredServerResolveProtocols(id.protocols))
	}
	return newctx, id.settingsPublisher, id.settingsName, otherOpts, nil
}

func (r *Runtime) WithNewServer(ctx *context.T, name string, object interface{}, auth security.Authorizer, opts ...rpc.ServerOpt) (*context.T, rpc.Server, error) {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	if transitionState >= xservers {
		// TODO(mattr): Deal with shutdown deps.
		newctx, spub, sname, opts, err := r.commonServerInit(ctx, opts...)
		if err != nil {
			return ctx, nil, err
		}
		s, err := irpc.NewServer(newctx, name, object, auth, spub, sname, opts...)
		if err != nil {
			// TODO(mattr): Stop the flow manager.
			return ctx, nil, err
		}
		return newctx, s, nil
	}
	s, err := r.NewServer(ctx, opts...)
	if err != nil {
		return ctx, nil, err
	}
	if _, err = s.Listen(r.GetListenSpec(ctx)); err != nil {
		s.Stop()
		return ctx, nil, err
	}
	if err = s.Serve(name, object, auth); err != nil {
		s.Stop()
		return ctx, nil, err
	}
	return ctx, s, nil
}

func (r *Runtime) WithNewDispatchingServer(ctx *context.T, name string, disp rpc.Dispatcher, opts ...rpc.ServerOpt) (*context.T, rpc.Server, error) {
	defer apilog.LogCall(ctx)(ctx) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	if transitionState >= xservers {
		// TODO(mattr): Deal with shutdown deps.
		newctx, spub, sname, opts, err := r.commonServerInit(ctx, opts...)
		if err != nil {
			return ctx, nil, err
		}
		s, err := irpc.NewDispatchingServer(newctx, name, disp, spub, sname, opts...)
		if err != nil {
			// TODO(mattr): Stop the flow manager.
			return ctx, nil, err
		}
		return newctx, s, nil
	}

	s, err := r.NewServer(ctx, opts...)
	if err != nil {
		return ctx, nil, err
	}
	if _, err = s.Listen(r.GetListenSpec(ctx)); err != nil {
		return ctx, nil, err
	}
	if err = s.ServeDispatcher(name, disp); err != nil {
		s.Stop()
		return ctx, nil, err
	}
	return ctx, s, nil
}

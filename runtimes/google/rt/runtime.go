package rt

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	ns "v.io/v23/naming/ns"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vlog"
	"v.io/v23/vtrace"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/flags/buildinfo"
	"v.io/core/veyron/lib/stats"
	_ "v.io/core/veyron/lib/stats/sysstats"
	iipc "v.io/core/veyron/runtimes/google/ipc"
	"v.io/core/veyron/runtimes/google/ipc/stream"
	imanager "v.io/core/veyron/runtimes/google/ipc/stream/manager"
	"v.io/core/veyron/runtimes/google/ipc/stream/vc"
	"v.io/core/veyron/runtimes/google/lib/dependency"
	inaming "v.io/core/veyron/runtimes/google/naming"
	"v.io/core/veyron/runtimes/google/naming/namespace"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"
)

type contextKey int

const (
	streamManagerKey = contextKey(iota)
	clientKey
	namespaceKey
	principalKey
	reservedNameKey
	profileKey
	appCycleKey
	listenSpecKey
	protocolsKey
	backgroundKey
)

type vtraceDependency struct{}

// Runtime implements the v23.Runtime interface.
// Please see the interface definition for documentation of the
// individiual methods.
type Runtime struct {
	deps *dependency.Graph
}

type reservedNameDispatcher struct {
	dispatcher ipc.Dispatcher
	opts       []ipc.ServerOpt
}

func Init(ctx *context.T, appCycle v23.AppCycle, protocols []string, listenSpec *ipc.ListenSpec, flags flags.RuntimeFlags,
	reservedDispatcher ipc.Dispatcher, dispatcherOpts ...ipc.ServerOpt) (*Runtime, *context.T, v23.Shutdown, error) {
	r := &Runtime{deps: dependency.NewGraph()}

	err := vlog.ConfigureLibraryLoggerFromFlags()
	if err != nil {
		return nil, nil, nil, err
	}
	// TODO(caprita): Only print this out for servers?
	vlog.Infof("Binary info: %s", buildinfo.Info())

	// Setup the initial trace.
	ctx, err = ivtrace.Init(ctx, flags.Vtrace)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, _ = vtrace.SetNewTrace(ctx)
	r.addChild(ctx, vtraceDependency{}, func() {
		vtrace.FormatTraces(os.Stderr, vtrace.GetStore(ctx).TraceRecords(), nil)
	})

	if reservedDispatcher != nil {
		ctx = context.WithValue(ctx, reservedNameKey, &reservedNameDispatcher{reservedDispatcher, dispatcherOpts})
	}

	if appCycle != nil {
		ctx = context.WithValue(ctx, appCycleKey, appCycle)
	}

	if len(protocols) > 0 {
		ctx = context.WithValue(ctx, protocolsKey, protocols)
	}

	if listenSpec != nil {
		ctx = context.WithValue(ctx, listenSpecKey, listenSpec)
	}

	// Setup i18n.
	ctx = i18n.ContextWithLangID(ctx, i18n.LangIDFromEnv())
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
	ctx = verror.ContextWithComponentName(ctx, filepath.Base(os.Args[0]))

	// Enable signal handling.
	r.initSignalHandling(ctx)

	// Set the initial namespace.
	ctx, _, err = r.setNewNamespace(ctx, flags.NamespaceRoots...)
	if err != nil {
		return nil, nil, nil, err
	}

	// Set the initial stream manager.
	ctx, err = r.setNewStreamManager(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	// The client we create here is incomplete (has a nil principal) and only works
	// because the agent uses anonymous unix sockets and VCSecurityNone.
	// After security is initialized we attach a real client.
	_, client, err := r.SetNewClient(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	// Initialize security.
	principal, err := initSecurity(ctx, flags.Credentials, client)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx = r.setPrincipal(ctx, principal)

	// Set up secure client.
	ctx, _, err = r.SetNewClient(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	ctx = r.SetBackgroundContext(ctx)

	return r, ctx, r.shutdown, nil
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
	return r.initMgmt(ctx)
}

func (r *Runtime) shutdown() {
	r.deps.CloseAndWaitForAll()
	vlog.FlushLog()
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
			vlog.Infof("Received signal %v", sig)
		}
	}()
	r.addChild(ctx, signals, func() {
		signal.Stop(signals)
		close(signals)
	})
}

func (*Runtime) NewEndpoint(ep string) (naming.Endpoint, error) {
	return inaming.NewEndpoint(ep)
}

func (r *Runtime) NewServer(ctx *context.T, opts ...ipc.ServerOpt) (ipc.Server, error) {
	// Create a new RoutingID (and StreamManager) for each server.
	sm, err := newStreamManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create ipc/stream/Manager: %v", err)
	}

	ns, _ := ctx.Value(namespaceKey).(ns.Namespace)
	principal, _ := ctx.Value(principalKey).(security.Principal)
	client, _ := ctx.Value(clientKey).(ipc.Client)

	otherOpts := append([]ipc.ServerOpt{}, opts...)
	otherOpts = append(otherOpts, vc.LocalPrincipal{principal})
	if reserved, ok := ctx.Value(reservedNameKey).(*reservedNameDispatcher); ok {
		otherOpts = append(otherOpts, iipc.ReservedNameDispatcher{reserved.dispatcher})
		otherOpts = append(otherOpts, reserved.opts...)
	}
	if protocols, ok := ctx.Value(protocolsKey).([]string); ok {
		otherOpts = append(otherOpts, iipc.PreferredServerResolveProtocols(protocols))
	}

	if !hasServerBlessingsOpt(opts) && principal != nil {
		otherOpts = append(otherOpts, options.ServerBlessings{principal.BlessingStore().Default()})
	}
	server, err := iipc.InternalNewServer(ctx, sm, ns, r.GetClient(ctx), otherOpts...)
	if err != nil {
		return nil, err
	}
	stop := func() {
		if err := server.Stop(); err != nil {
			vlog.Errorf("A server could not be stopped: %v", err)
		}
		sm.Shutdown()
	}
	if err = r.addChild(ctx, server, stop, client, vtraceDependency{}); err != nil {
		return nil, err
	}
	return server, nil
}

func hasServerBlessingsOpt(opts []ipc.ServerOpt) bool {
	for _, o := range opts {
		if _, ok := o.(options.ServerBlessings); ok {
			return true
		}
	}
	return false
}

func newStreamManager() (stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	sm := imanager.InternalNew(rid)
	return sm, nil
}

func (r *Runtime) setNewStreamManager(ctx *context.T) (*context.T, error) {
	sm, err := newStreamManager()
	if err != nil {
		return nil, err
	}
	newctx := context.WithValue(ctx, streamManagerKey, sm)
	if err = r.addChild(ctx, sm, sm.Shutdown); err != nil {
		return ctx, err
	}
	return newctx, err
}

func (r *Runtime) SetNewStreamManager(ctx *context.T) (*context.T, error) {
	newctx, err := r.setNewStreamManager(ctx)
	if err != nil {
		return ctx, err
	}

	// Create a new client since it depends on the stream manager.
	newctx, _, err = r.SetNewClient(newctx)
	if err != nil {
		return ctx, err
	}
	return newctx, nil
}

func (*Runtime) setPrincipal(ctx *context.T, principal security.Principal) *context.T {
	// We uniquely identity a principal with "security/principal/<publicKey>"
	principalName := "security/principal/" + principal.PublicKey().String()
	stats.NewStringFunc(principalName+"/blessingstore", principal.BlessingStore().DebugString)
	stats.NewStringFunc(principalName+"/blessingroots", principal.Roots().DebugString)
	return context.WithValue(ctx, principalKey, principal)
}

func (r *Runtime) SetPrincipal(ctx *context.T, principal security.Principal) (*context.T, error) {
	var err error
	newctx := ctx

	newctx = r.setPrincipal(ctx, principal)

	if newctx, err = r.setNewStreamManager(newctx); err != nil {
		return ctx, err
	}
	if newctx, _, err = r.setNewNamespace(newctx, r.GetNamespace(ctx).Roots()...); err != nil {
		return ctx, err
	}
	if newctx, _, err = r.SetNewClient(newctx); err != nil {
		return ctx, err
	}

	return newctx, nil
}

func (*Runtime) GetPrincipal(ctx *context.T) security.Principal {
	p, _ := ctx.Value(principalKey).(security.Principal)
	return p
}

func (r *Runtime) SetNewClient(ctx *context.T, opts ...ipc.ClientOpt) (*context.T, ipc.Client, error) {
	otherOpts := append([]ipc.ClientOpt{}, opts...)

	sm, _ := ctx.Value(streamManagerKey).(stream.Manager)
	ns, _ := ctx.Value(namespaceKey).(ns.Namespace)
	p, _ := ctx.Value(principalKey).(security.Principal)
	otherOpts = append(otherOpts, vc.LocalPrincipal{p}, &imanager.DialTimeout{5 * time.Minute})

	if protocols, ok := ctx.Value(protocolsKey).([]string); ok {
		otherOpts = append(otherOpts, iipc.PreferredProtocols(protocols))
	}

	client, err := iipc.InternalNewClient(sm, ns, otherOpts...)
	if err != nil {
		return ctx, nil, err
	}
	newctx := context.WithValue(ctx, clientKey, client)
	if err = r.addChild(ctx, client, client.Close, sm, vtraceDependency{}); err != nil {
		return ctx, nil, err
	}
	return newctx, client, err
}

func (*Runtime) GetClient(ctx *context.T) ipc.Client {
	cl, _ := ctx.Value(clientKey).(ipc.Client)
	return cl
}

func (r *Runtime) setNewNamespace(ctx *context.T, roots ...string) (*context.T, ns.Namespace, error) {
	ns, err := namespace.New(roots...)

	if oldNS := r.GetNamespace(ctx); oldNS != nil {
		ns.CacheCtl(oldNS.CacheCtl()...)
	}

	if err == nil {
		ctx = context.WithValue(ctx, namespaceKey, ns)
	}
	return ctx, ns, err
}

func (r *Runtime) SetNewNamespace(ctx *context.T, roots ...string) (*context.T, ns.Namespace, error) {
	newctx, ns, err := r.setNewNamespace(ctx, roots...)
	if err != nil {
		return ctx, nil, err
	}

	// Replace the client since it depends on the namespace.
	newctx, _, err = r.SetNewClient(newctx)
	if err != nil {
		return ctx, nil, err
	}

	return newctx, ns, err
}

func (*Runtime) GetNamespace(ctx *context.T) ns.Namespace {
	ns, _ := ctx.Value(namespaceKey).(ns.Namespace)
	return ns
}

func (*Runtime) GetAppCycle(ctx *context.T) v23.AppCycle {
	appCycle, _ := ctx.Value(appCycleKey).(v23.AppCycle)
	return appCycle
}

func (*Runtime) GetListenSpec(ctx *context.T) ipc.ListenSpec {
	listenSpec, _ := ctx.Value(listenSpecKey).(*ipc.ListenSpec)
	return *listenSpec
}

func (*Runtime) SetBackgroundContext(ctx *context.T) *context.T {
	// Note we add an extra context with a nil value here.
	// This prevents users from travelling back through the
	// chain of background contexts.
	ctx = context.WithValue(ctx, backgroundKey, nil)
	return context.WithValue(ctx, backgroundKey, ctx)
}

func (*Runtime) GetBackgroundContext(ctx *context.T) *context.T {
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

package rt

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/i18n"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/ipc/stream"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"

	"v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/flags"
	_ "v.io/core/veyron/lib/stats/sysstats"
	iipc "v.io/core/veyron/runtimes/google/ipc"
	imanager "v.io/core/veyron/runtimes/google/ipc/stream/manager"
	"v.io/core/veyron/runtimes/google/ipc/stream/vc"
	inaming "v.io/core/veyron/runtimes/google/naming"
	"v.io/core/veyron/runtimes/google/naming/namespace"
	ivtrace "v.io/core/veyron/runtimes/google/vtrace"
)

type contextKey int

const (
	streamManagerKey = contextKey(iota)
	clientKey
	namespaceKey
	loggerKey
	principalKey
	reservedNameKey
	profileKey
	appCycleKey
	listenSpecKey
	protocolsKey
	backgroundKey
)

type vtraceDependency struct{}

// TODO(suharshs,mattr): Panic instead of flagsOnce after the transition to veyron.Init is completed.
var flagsOnce sync.Once
var runtimeFlags *flags.Flags

func init() {
	runtimeFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime)
}

type depSet struct {
	count int
	cond  *sync.Cond
}

// RuntimeX implements the veyron2.RuntimeX interface.  It is stateless.
// Please see the interface definition for documentation of the
// individiual methods.
type RuntimeX struct {
	mu   sync.Mutex
	deps map[interface{}]*depSet
}

type reservedNameDispatcher struct {
	dispatcher ipc.Dispatcher
	opts       []ipc.ServerOpt
}

// TODO(mattr,suharshs): Decide if ROpts would be better than this.
func Init(ctx *context.T, appCycle veyron2.AppCycle, protocols []string, listenSpec *ipc.ListenSpec, reservedDispatcher ipc.Dispatcher, dispatcherOpts ...ipc.ServerOpt) (*RuntimeX, *context.T, veyron2.Shutdown, error) {
	r := &RuntimeX{deps: make(map[interface{}]*depSet)}
	r.newDepSetLocked(r)

	handle, err := exec.GetChildHandle()
	switch err {
	case exec.ErrNoVersion:
		// The process has not been started through the veyron exec
		// library. No further action is needed.
	case nil:
		// The process has been started through the veyron exec
		// library.
	default:
		return nil, nil, nil, err
	}

	// Parse runtime flags.
	flagsOnce.Do(func() {
		var config map[string]string
		if handle != nil {
			config = handle.Config.Dump()
		}
		runtimeFlags.Parse(os.Args[1:], config)
	})
	flags := runtimeFlags.RuntimeFlags()

	r.initLogging(ctx)
	ctx = context.WithValue(ctx, loggerKey, vlog.Log)

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
	ctx = verror2.ContextWithComponentName(ctx, filepath.Base(os.Args[0]))

	// Enable signal handling.
	r.initSignalHandling(ctx)

	// Set the initial namespace.
	ctx, _, err = r.setNewNamespace(ctx, flags.NamespaceRoots...)
	if err != nil {
		return nil, nil, nil, err
	}

	// Set the initial stream manager.
	ctx, _, err = r.setNewStreamManager(ctx)
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
	principal, err := initSecurity(ctx, handle, flags.Credentials, client)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx = context.WithValue(ctx, principalKey, principal)

	// Set up secure client.
	ctx, _, err = r.SetNewClient(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	// Initialize management.
	if err := r.initMgmt(ctx, r.GetAppCycle(ctx), handle); err != nil {
		return nil, nil, nil, err
	}

	ctx = r.SetBackgroundContext(ctx)

	// TODO(suharshs,mattr): Go through the rt.Cleanup function and make sure everything
	// gets cleaned up.

	if handle != nil {
		handle.SetReady()
	}

	return r, ctx, r.cancel, nil
}

func (r *RuntimeX) addChild(ctx *context.T, me interface{}, stop func(), dependsOn ...interface{}) error {
	// Note that we keep a depSet for the runtime itself
	// (which we say every child depends on) and we use that to determine
	// when the runtime can be cleaned up.
	dependsOn = append(dependsOn, r)

	r.mu.Lock()
	r.newDepSetLocked(me)
	deps, err := r.getDepsLocked(dependsOn)
	if err != nil {
		r.mu.Unlock()
		stop()
		return err
	}
	r.incrLocked(deps)
	r.mu.Unlock()

	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			r.wait(me)
			stop()
			r.decr(deps)
		}()
	}
	return nil
}

func (r *RuntimeX) newDepSetLocked(key interface{}) {
	r.deps[key] = &depSet{cond: sync.NewCond(&r.mu)}
}

func (r *RuntimeX) getDepsLocked(keys []interface{}) ([]*depSet, error) {
	out := make([]*depSet, len(keys))
	for i := range keys {
		out[i] = r.deps[keys[i]]
		if out[i] == nil {
			return nil, fmt.Errorf("You are creating an object but it depends on something that is already shutdown: %v.", keys[i])
		}
	}
	return out, nil
}

func (r *RuntimeX) incrLocked(sets []*depSet) {
	for _, ds := range sets {
		ds.count++
	}
}

func (r *RuntimeX) decr(sets []*depSet) {
	r.mu.Lock()
	for _, ds := range sets {
		ds.count--
		if ds.count == 0 {
			ds.cond.Broadcast()
		}
	}
	r.mu.Unlock()
}

func (r *RuntimeX) wait(key interface{}) {
	r.mu.Lock()
	ds := r.deps[key]
	if ds == nil {
		panic(fmt.Sprintf("ds is gone for %#v", key))
	}
	delete(r.deps, key)
	for ds.count > 0 {
		ds.cond.Wait()
	}
	r.mu.Unlock()
}

func (r *RuntimeX) cancel() {
	r.wait(r)
	vlog.FlushLog()
}

// initLogging configures logging for the runtime. It needs to be called after
// flag.Parse and after signal handling has been initialized.
func (r *RuntimeX) initLogging(ctx *context.T) error {
	return vlog.ConfigureLibraryLoggerFromFlags()
}

func (r *RuntimeX) initSignalHandling(ctx *context.T) {
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

func (*RuntimeX) NewEndpoint(ep string) (naming.Endpoint, error) {
	return inaming.NewEndpoint(ep)
}

func (r *RuntimeX) NewServer(ctx *context.T, opts ...ipc.ServerOpt) (ipc.Server, error) {
	// Create a new RoutingID (and StreamManager) for each server.
	sm, err := newStreamManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create ipc/stream/Manager: %v", err)
	}

	ns, _ := ctx.Value(namespaceKey).(naming.Namespace)
	principal, _ := ctx.Value(principalKey).(security.Principal)
	client, _ := ctx.Value(clientKey).(ipc.Client)

	otherOpts := append([]ipc.ServerOpt{}, opts...)
	otherOpts = append(otherOpts, vc.LocalPrincipal{principal})
	if reserved, ok := ctx.Value(reservedNameKey).(*reservedNameDispatcher); ok {
		otherOpts = append(otherOpts, options.ReservedNameDispatcher{reserved.dispatcher})
		otherOpts = append(otherOpts, reserved.opts...)
	}
	if protocols, ok := ctx.Value(protocolsKey).([]string); ok {
		otherOpts = append(otherOpts, iipc.PreferredServerResolveProtocols(protocols))
	}
	server, err := iipc.InternalNewServer(ctx, sm, ns, otherOpts...)
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

func newStreamManager(opts ...stream.ManagerOpt) (stream.Manager, error) {
	rid, err := naming.NewRoutingID()
	if err != nil {
		return nil, err
	}
	sm := imanager.InternalNew(rid)
	return sm, nil
}

func (r *RuntimeX) setNewStreamManager(ctx *context.T, opts ...stream.ManagerOpt) (*context.T, stream.Manager, error) {
	sm, err := newStreamManager(opts...)
	newctx := context.WithValue(ctx, streamManagerKey, sm)
	if err = r.addChild(ctx, sm, sm.Shutdown); err != nil {
		return ctx, nil, err
	}
	return newctx, sm, err
}

func (r *RuntimeX) SetNewStreamManager(ctx *context.T, opts ...stream.ManagerOpt) (*context.T, stream.Manager, error) {
	newctx, sm, err := r.setNewStreamManager(ctx, opts...)
	if err != nil {
		return ctx, nil, err
	}

	// Create a new client since it depends on the stream manager.
	newctx, _, err = r.SetNewClient(newctx)
	if err != nil {
		return ctx, nil, err
	}
	return newctx, sm, nil
}

func (*RuntimeX) GetStreamManager(ctx *context.T) stream.Manager {
	cl, _ := ctx.Value(streamManagerKey).(stream.Manager)
	return cl
}

func (r *RuntimeX) SetPrincipal(ctx *context.T, principal security.Principal) (*context.T, error) {
	var err error
	newctx := ctx

	newctx = context.WithValue(newctx, principalKey, principal)

	// TODO(mattr, suharshs): The stream manager holds a cache of vifs
	// which were negotiated with the principal, so we replace it here when the
	// principal changes.  However we should negotiate the vif with a
	// random principal and then we needn't replace this here.
	if newctx, _, err = r.setNewStreamManager(newctx); err != nil {
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

func (*RuntimeX) GetPrincipal(ctx *context.T) security.Principal {
	p, _ := ctx.Value(principalKey).(security.Principal)
	return p
}

func (r *RuntimeX) SetNewClient(ctx *context.T, opts ...ipc.ClientOpt) (*context.T, ipc.Client, error) {
	otherOpts := append([]ipc.ClientOpt{}, opts...)

	// TODO(mattr, suharshs):  Currently there are a lot of things that can come in as opts.
	// Some of them will be removed as opts and simply be pulled from the context instead
	// these are:
	// stream.Manager, Namespace, LocalPrincipal, preferred protocols.
	sm, _ := ctx.Value(streamManagerKey).(stream.Manager)
	ns, _ := ctx.Value(namespaceKey).(naming.Namespace)
	p, _ := ctx.Value(principalKey).(security.Principal)
	otherOpts = append(otherOpts, vc.LocalPrincipal{p}, &imanager.DialTimeout{5 * time.Minute})

	if protocols, ok := ctx.Value(protocolsKey).([]string); ok {
		otherOpts = append(otherOpts, options.PreferredProtocols(protocols))
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

func (*RuntimeX) GetClient(ctx *context.T) ipc.Client {
	cl, _ := ctx.Value(clientKey).(ipc.Client)
	return cl
}

func (*RuntimeX) setNewNamespace(ctx *context.T, roots ...string) (*context.T, naming.Namespace, error) {
	ns, err := namespace.New(roots...)
	// TODO(mattr): Copy cache settings.
	if err == nil {
		ctx = context.WithValue(ctx, namespaceKey, ns)
	}
	return ctx, ns, err
}

func (r *RuntimeX) SetNewNamespace(ctx *context.T, roots ...string) (*context.T, naming.Namespace, error) {
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

func (*RuntimeX) GetNamespace(ctx *context.T) naming.Namespace {
	ns, _ := ctx.Value(namespaceKey).(naming.Namespace)
	return ns
}

func (*RuntimeX) SetNewLogger(ctx *context.T, name string, opts ...vlog.LoggingOpts) (*context.T, vlog.Logger, error) {
	logger, err := vlog.NewLogger(name, opts...)
	if err == nil {
		ctx = context.WithValue(ctx, loggerKey, logger)
	}
	return ctx, logger, err
}

func (*RuntimeX) GetLogger(ctx *context.T) vlog.Logger {
	logger, _ := ctx.Value(loggerKey).(vlog.Logger)
	return logger
}

// SetProfile sets the profile used to create this runtime.
// TODO(suharshs, mattr): Determine if this is needed by functions after the new
// profile init function is in use. This will probably be easy to do because:
// Name is used in tests only.
// Platform is used for String representaions of a Profile.
// String is unused.
// Cleanup is used in rt.Cleanup and can probably be replaced by a cancelfunc returned
// by the new profile initialization function.
func (*RuntimeX) SetProfile(ctx *context.T, profile veyron2.Profile) *context.T {
	return context.WithValue(ctx, profileKey, profile)
}

func (*RuntimeX) GetProfile(ctx *context.T) veyron2.Profile {
	profile, _ := ctx.Value(profileKey).(veyron2.Profile)
	return profile
}

func (*RuntimeX) GetAppCycle(ctx *context.T) veyron2.AppCycle {
	appCycle, _ := ctx.Value(appCycleKey).(veyron2.AppCycle)
	return appCycle
}

func (*RuntimeX) GetListenSpec(ctx *context.T) ipc.ListenSpec {
	listenSpec, _ := ctx.Value(listenSpecKey).(*ipc.ListenSpec)
	return *listenSpec
}

func (*RuntimeX) SetBackgroundContext(ctx *context.T) *context.T {
	// Note we add an extra context with a nil value here.
	// This prevents users from travelling back through the
	// chain of background contexts.
	ctx = context.WithValue(ctx, backgroundKey, nil)
	return context.WithValue(ctx, backgroundKey, ctx)
}

func (*RuntimeX) GetBackgroundContext(ctx *context.T) *context.T {
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

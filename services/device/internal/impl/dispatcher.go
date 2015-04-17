// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/device"
	"v.io/v23/services/pprof"
	"v.io/v23/services/stats"
	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/agent/agentlib"
	"v.io/x/ref/services/agent/keymgr"
	s_device "v.io/x/ref/services/device"
	"v.io/x/ref/services/device/internal/config"
	"v.io/x/ref/services/internal/acls"
	"v.io/x/ref/services/internal/logreaderlib"
)

// internalState wraps state shared between different device manager
// invocations.
type internalState struct {
	callback       *callbackState
	updating       *updatingState
	securityAgent  *securityAgentState
	restartHandler func()
	testMode       bool
}

// dispatcher holds the state of the device manager dispatcher.
type dispatcher struct {
	// internal holds the state that persists across RPC method invocations.
	internal *internalState
	// config holds the device manager's (immutable) configuration state.
	config *config.State
	// dispatcherMutex is a lock for coordinating concurrent access to some
	// dispatcher methods.
	mu sync.RWMutex
	// TODO(rjkroege): Consider moving this inside internal.
	uat      BlessingSystemAssociationStore
	aclstore *acls.PathStore
	// Namespace
	mtAddress string // The address of the local mounttable.
	// reap is the app process monitoring subsystem.
	reap reaper
}

var _ rpc.Dispatcher = (*dispatcher)(nil)

const (
	appsSuffix   = "apps"
	deviceSuffix = "device"
	configSuffix = "cfg"

	pkgPath = "v.io/x/ref/services/device/internal/impl"
)

var (
	ErrInvalidSuffix        = verror.Register(pkgPath+".InvalidSuffix", verror.NoRetry, "{1:}{2:} invalid suffix{:_}")
	ErrOperationFailed      = verror.Register(pkgPath+".OperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
	ErrOperationInProgress  = verror.Register(pkgPath+".OperationInProgress", verror.NoRetry, "{1:}{2:} operation in progress{:_}")
	ErrAppTitleMismatch     = verror.Register(pkgPath+".AppTitleMismatch", verror.NoRetry, "{1:}{2:} app title mismatch{:_}")
	ErrUpdateNoOp           = verror.Register(pkgPath+".UpdateNoOp", verror.NoRetry, "{1:}{2:} update is no op{:_}")
	ErrInvalidOperation     = verror.Register(pkgPath+".InvalidOperation", verror.NoRetry, "{1:}{2:} invalid operation{:_}")
	ErrInvalidBlessing      = verror.Register(pkgPath+".InvalidBlessing", verror.NoRetry, "{1:}{2:} invalid blessing{:_}")
	ErrInvalidPairingToken  = verror.Register(pkgPath+".InvalidPairingToken", verror.NoRetry, "{1:}{2:} pairing token mismatch{:_}")
	ErrUnclaimedDevice      = verror.Register(pkgPath+".UnclaimedDevice", verror.NoRetry, "{1:}{2:} device needs to be claimed first")
	ErrDeviceAlreadyClaimed = verror.Register(pkgPath+".AlreadyClaimed", verror.NoRetry, "{1:}{2:} device has already been claimed")

	errInvalidConfig          = verror.Register(pkgPath+".errInvalidConfig", verror.NoRetry, "{1:}{2:} invalid config {3}{:_}")
	errCantCreateAccountStore = verror.Register(pkgPath+".errCantCreateAccountStore", verror.NoRetry, "{1:}{2:} cannot create persistent store for identity to system account associations{:_}")
	errCantCreateAppWatcher   = verror.Register(pkgPath+".errCantCreateAppWatcher", verror.NoRetry, "{1:}{2:} cannot create app status watcher{:_}")
	errNewAgentFailed         = verror.Register(pkgPath+".errNewAgentFailed", verror.NoRetry, "{1:}{2:} NewAgent() failed{:_}")
)

// NewClaimableDispatcher returns an rpc.Dispatcher that allows the device to
// be Claimed if it hasn't been already and a channel that will be closed once
// the device has been claimed.
//
// It returns (nil, nil) if the device is no longer claimable.
func NewClaimableDispatcher(ctx *context.T, config *config.State, pairingToken string) (rpc.Dispatcher, <-chan struct{}) {
	var (
		aclDir   = AclDir(config)
		aclstore = acls.NewPathStore(v23.GetPrincipal(ctx))
	)
	if _, _, err := aclstore.Get(aclDir); !os.IsNotExist(err) {
		return nil, nil
	}
	// The device is claimable only if Claim hasn't been called before. The
	// existence of the AccessList file is an indication of a successful prior
	// call to Claim.
	notify := make(chan struct{})
	return &claimable{token: pairingToken, aclstore: aclstore, aclDir: aclDir, notify: notify}, notify
}

// NewDispatcher is the device manager dispatcher factory.
func NewDispatcher(ctx *context.T, config *config.State, mtAddress string, testMode bool, restartHandler func(), permStore *acls.PathStore) (rpc.Dispatcher, error) {
	if err := config.Validate(); err != nil {
		return nil, verror.New(errInvalidConfig, ctx, config, err)
	}
	uat, err := NewBlessingSystemAssociationStore(config.Root)
	if err != nil {
		return nil, verror.New(errCantCreateAccountStore, ctx, err)
	}
	reap, err := newReaper(ctx, config.Root)
	if err != nil {
		return nil, verror.New(errCantCreateAppWatcher, ctx, err)
	}
	initSuidHelper(config.Helper)
	d := &dispatcher{
		internal: &internalState{
			callback:       newCallbackState(config.Name),
			updating:       newUpdatingState(),
			restartHandler: restartHandler,
			testMode:       testMode,
		},
		config:    config,
		uat:       uat,
		aclstore:  permStore,
		mtAddress: mtAddress,
		reap:      reap,
	}

	// If we're in 'security agent mode', set up the key manager agent.
	if len(os.Getenv(agentlib.FdVarName)) > 0 {
		if keyMgrAgent, err := keymgr.NewAgent(); err != nil {
			return nil, verror.New(errNewAgentFailed, ctx, err)
		} else {
			d.internal.securityAgent = &securityAgentState{
				keyMgrAgent: keyMgrAgent,
			}
		}
	}
	if testMode {
		return &testModeDispatcher{d}, nil
	}
	return d, nil
}

// Shutdown the dispatcher.
func Shutdown(rpcd rpc.Dispatcher) {
	switch d := rpcd.(type) {
	case *dispatcher:
		d.reap.shutdown()
	case *testModeDispatcher:
		Shutdown(d.realDispatcher)
	default:
		vlog.Panicf("%v not a supported dispatcher type.", rpcd)
	}
}

// Logging invoker that logs any error messages before returning.
func newLoggingInvoker(obj interface{}) (rpc.Invoker, error) {
	if invoker, ok := obj.(rpc.Invoker); ok {
		return &loggingInvoker{invoker}, nil
	}
	invoker, err := rpc.ReflectInvoker(obj)
	if err != nil {
		vlog.Errorf("rpc.ReflectInvoker returned error: %v", err)
		return nil, err
	}
	return &loggingInvoker{invoker}, nil
}

type loggingInvoker struct {
	invoker rpc.Invoker
}

func (l *loggingInvoker) Prepare(method string, numArgs int) (argptrs []interface{}, tags []*vdl.Value, err error) {
	argptrs, tags, err = l.invoker.Prepare(method, numArgs)
	if err != nil {
		vlog.Errorf("Prepare(%s %d) returned error: %v", method, numArgs, err)
	}
	return
}

func (l *loggingInvoker) Invoke(ctx *context.T, call rpc.StreamServerCall, method string, argptrs []interface{}) (results []interface{}, err error) {
	results, err = l.invoker.Invoke(ctx, call, method, argptrs)
	if err != nil {
		vlog.Errorf("Invoke(method:%s argptrs:%v) returned error: %v", method, argptrs, err)
	}
	return
}

func (l *loggingInvoker) Signature(ctx *context.T, call rpc.ServerCall) ([]signature.Interface, error) {
	sig, err := l.invoker.Signature(ctx, call)
	if err != nil {
		vlog.Errorf("Signature returned error: %v", err)
	}
	return sig, err
}

func (l *loggingInvoker) MethodSignature(ctx *context.T, call rpc.ServerCall, method string) (signature.Method, error) {
	methodSig, err := l.invoker.MethodSignature(ctx, call, method)
	if err != nil {
		vlog.Errorf("MethodSignature(%s) returned error: %v", method, err)
	}
	return methodSig, err
}

func (l *loggingInvoker) Globber() *rpc.GlobState {
	return l.invoker.Globber()
}

// DISPATCHER INTERFACE IMPLEMENTATION
func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	invoker, auth, err := d.internalLookup(suffix)
	if err != nil {
		return nil, nil, err
	}
	loggingInvoker, err := newLoggingInvoker(invoker)
	if err != nil {
		return nil, nil, err
	}
	return loggingInvoker, auth, nil
}

func newTestableHierarchicalAuth(testMode bool, rootDir, childDir string, get acls.TAMGetter) (security.Authorizer, error) {
	if testMode {
		// In test mode, the device manager will not be able to read
		// the AccessLists, because they were signed with the key of the real
		// device manager. It's not a problem because the
		// testModeDispatcher overrides the authorizer anyway.
		return nil, nil
	}
	return acls.NewHierarchicalAuthorizer(rootDir, childDir, get)
}

func (d *dispatcher) internalLookup(suffix string) (interface{}, security.Authorizer, error) {
	components := strings.Split(suffix, "/")
	for i := 0; i < len(components); i++ {
		if len(components[i]) == 0 {
			components = append(components[:i], components[i+1:]...)
			i--
		}
	}

	// TODO(rjkroege): Permit the root AccessLists to diverge for the
	// device and app sub-namespaces of the device manager after
	// claiming.
	auth, err := newTestableHierarchicalAuth(d.internal.testMode, AclDir(d.config), AclDir(d.config), d.aclstore)
	if err != nil {
		return nil, nil, err
	}

	if len(components) == 0 {
		return rpc.ChildrenGlobberInvoker(deviceSuffix, appsSuffix), auth, nil
	}
	// The implementation of the device manager is split up into several
	// invokers, which are instantiated depending on the receiver name
	// prefix.
	switch components[0] {
	case deviceSuffix:
		receiver := device.DeviceServer(&deviceService{
			callback:       d.internal.callback,
			updating:       d.internal.updating,
			restartHandler: d.internal.restartHandler,
			config:         d.config,
			disp:           d,
			uat:            d.uat,
			securityAgent:  d.internal.securityAgent,
		})
		return receiver, auth, nil
	case appsSuffix:
		// Requests to apps/*/*/*/logs are handled locally by LogFileService.
		// Requests to apps/*/*/*/pprof are proxied to the apps' __debug/pprof object.
		// Requests to apps/*/*/*/stats are proxied to the apps' __debug/stats object.
		// Everything else is handled by the Application server.
		if len(components) >= 5 {
			appInstanceDir, err := instanceDir(d.config.Root, components[1:4])
			if err != nil {
				return nil, nil, err
			}
			switch kind := components[4]; kind {
			case "logs":
				logsDir := filepath.Join(appInstanceDir, "logs")
				suffix := naming.Join(components[5:]...)
				appSpecificAuthorizer, err := newAppSpecificAuthorizer(auth, d.config, components[1:], d.aclstore)
				if err != nil {
					return nil, nil, err
				}
				return logreaderlib.NewLogFileService(logsDir, suffix), appSpecificAuthorizer, nil
			case "pprof", "stats":
				info, err := loadInstanceInfo(nil, appInstanceDir)
				if err != nil {
					return nil, nil, err
				}
				if !instanceStateIs(appInstanceDir, device.InstanceStateStarted) {
					return nil, nil, verror.New(ErrInvalidSuffix, nil)
				}
				var desc []rpc.InterfaceDesc
				switch kind {
				case "pprof":
					desc = pprof.PProfServer(nil).Describe__()
				case "stats":
					desc = stats.StatsServer(nil).Describe__()
				}
				suffix := naming.Join("__debug", naming.Join(components[4:]...))
				remote := naming.JoinAddressName(info.AppCycleMgrName, suffix)

				// Use hierarchical auth with debugacls under debug access.
				appSpecificAuthorizer, err := newAppSpecificAuthorizer(auth, d.config, components[1:], d.aclstore)
				if err != nil {
					return nil, nil, err
				}
				return newProxyInvoker(remote, access.Debug, desc), appSpecificAuthorizer, nil
			}
		}
		receiver := device.ApplicationServer(&appService{
			callback:      d.internal.callback,
			config:        d.config,
			suffix:        components[1:],
			uat:           d.uat,
			aclstore:      d.aclstore,
			securityAgent: d.internal.securityAgent,
			mtAddress:     d.mtAddress,
			reap:          d.reap,
		})
		appSpecificAuthorizer, err := newAppSpecificAuthorizer(auth, d.config, components[1:], d.aclstore)
		if err != nil {
			return nil, nil, err
		}
		return receiver, appSpecificAuthorizer, nil
	case configSuffix:
		if len(components) != 2 {
			return nil, nil, verror.New(ErrInvalidSuffix, nil)
		}
		receiver := s_device.ConfigServer(&configService{
			callback: d.internal.callback,
			suffix:   components[1],
		})
		// The nil authorizer ensures that only principals blessed by
		// the device manager can talk back to it.  All apps started by
		// the device manager should fall in that category.
		//
		// TODO(caprita,rjkroege): We should further refine this, by
		// only allowing the app to update state referring to itself
		// (and not other apps).
		return receiver, nil, nil
	default:
		return nil, nil, verror.New(ErrInvalidSuffix, nil)
	}
}

// testModeDispatcher is a wrapper around the real dispatcher. It returns the
// exact same object as the real dispatcher, but the authorizer only allows
// calls to "device".Stop().
type testModeDispatcher struct {
	realDispatcher rpc.Dispatcher
}

func (d *testModeDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	obj, _, err := d.realDispatcher.Lookup(suffix)
	return obj, d, err
}

func (testModeDispatcher) Authorize(ctx *context.T, call security.Call) error {
	if call.Suffix() == deviceSuffix && call.Method() == "Stop" {
		vlog.Infof("testModeDispatcher.Authorize: Allow %q.%s()", call.Suffix(), call.Method())
		return nil
	}
	vlog.Infof("testModeDispatcher.Authorize: Reject %q.%s()", call.Suffix(), call.Method())
	return verror.New(ErrInvalidSuffix, nil)
}

func newAppSpecificAuthorizer(sec security.Authorizer, config *config.State, suffix []string, getter acls.TAMGetter) (security.Authorizer, error) {
	// TODO(rjkroege): This does not support <appname>.Start() to start all
	// instances. Correct this.

	// If we are attempting a method invocation against "apps/", we use
	// the root AccessList.
	if len(suffix) == 0 || len(suffix) == 1 {
		return sec, nil
	}
	// Otherwise, we require a per-installation and per-instance AccessList file.
	if len(suffix) == 2 {
		p, err := installationDirCore(suffix, config.Root)
		if err != nil {
			return nil, verror.New(ErrOperationFailed, nil, fmt.Sprintf("newAppSpecificAuthorizer failed: %v", err))
		}
		return acls.NewHierarchicalAuthorizer(AclDir(config), path.Join(p, "acls"), getter)
	}
	// Use the special debugacls for instance/logs, instance/pprof, instance//stats.
	if len(suffix) > 3 && (suffix[3] == "logs" || suffix[3] == "pprof" || suffix[3] == "stats") {
		p, err := instanceDir(config.Root, suffix[0:3])
		if err != nil {
			return nil, verror.New(ErrOperationFailed, nil, fmt.Sprintf("newAppSpecificAuthorizer failed: %v", err))
		}
		return acls.NewHierarchicalAuthorizer(AclDir(config), path.Join(p, "debugacls"), getter)
	}

	p, err := instanceDir(config.Root, suffix[0:3])
	if err != nil {
		return nil, verror.New(ErrOperationFailed, nil, fmt.Sprintf("newAppSpecificAuthorizer failed: %v", err))
	}
	return acls.NewHierarchicalAuthorizer(AclDir(config), path.Join(p, "acls"), getter)
}

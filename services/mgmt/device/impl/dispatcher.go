package impl

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"v.io/core/veyron/security/agent"
	"v.io/core/veyron/security/agent/keymgr"
	idevice "v.io/core/veyron/services/mgmt/device"
	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/lib/acls"
	logsimpl "v.io/core/veyron/services/mgmt/logreader/impl"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/device"
	"v.io/v23/services/mgmt/pprof"
	"v.io/v23/services/mgmt/stats"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/v23/vlog"
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
	uat       BlessingSystemAssociationStore
	locks     *acls.Locks
	principal security.Principal
	// Namespace
	mtAddress string // The address of the local mounttable.
	// reap is the app process monitoring subsystem.
	reap reaper
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

const (
	appsSuffix   = "apps"
	deviceSuffix = "device"
	configSuffix = "cfg"

	pkgPath = "v.io/core/veyron/services/mgmt/device/impl"
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
)

// NewClaimableDispatcher returns an ipc.Dispatcher that allows the device to
// be Claimed if it hasn't been already and a channel that will be closed once
// the device has been claimed.
//
// It returns (nil, nil) if the device is no longer claimable.
func NewClaimableDispatcher(ctx *context.T, config *config.State, pairingToken string) (ipc.Dispatcher, <-chan struct{}) {
	var (
		principal = v23.GetPrincipal(ctx)
		aclDir    = aclDir(config)
		locks     = acls.NewLocks()
	)
	if _, _, err := locks.GetPathACL(principal, aclDir); !os.IsNotExist(err) {
		return nil, nil
	}
	// The device is claimable only if Claim hasn't been called before. The
	// existence of the ACL file is an indication of a successful prior
	// call to Claim.
	notify := make(chan struct{})
	return &claimable{token: pairingToken, locks: locks, aclDir: aclDir, notify: notify}, notify
}

// NewDispatcher is the device manager dispatcher factory.
func NewDispatcher(ctx *context.T, config *config.State, mtAddress string, testMode bool, restartHandler func()) (ipc.Dispatcher, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %v: %v", config, err)
	}
	uat, err := NewBlessingSystemAssociationStore(config.Root)
	if err != nil {
		return nil, fmt.Errorf("cannot create persistent store for identity to system account associations: %v", err)
	}
	reap, err := newReaper(ctx, config.Root)
	if err != nil {
		return nil, fmt.Errorf("cannot create app status watcher: %v", err)
	}
	d := &dispatcher{
		internal: &internalState{
			callback:       newCallbackState(config.Name),
			updating:       newUpdatingState(),
			restartHandler: restartHandler,
			testMode:       testMode,
		},
		config:    config,
		uat:       uat,
		locks:     acls.NewLocks(),
		principal: v23.GetPrincipal(ctx),
		mtAddress: mtAddress,
		reap:      reap,
	}

	// If we're in 'security agent mode', set up the key manager agent.
	if len(os.Getenv(agent.FdVarName)) > 0 {
		if keyMgrAgent, err := keymgr.NewAgent(); err != nil {
			return nil, fmt.Errorf("NewAgent() failed: %v", err)
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
func Shutdown(ipcd ipc.Dispatcher) {
	switch d := ipcd.(type) {
	case *dispatcher:
		d.reap.shutdown()
	case *testModeDispatcher:
		Shutdown(d.realDispatcher)
	default:
		vlog.Panicf("%v not a supported dispatcher type.", ipcd)
	}
}

// TODO(rjkroege): Consider refactoring authorizer implementations to
// be shareable with other components.
func (d *dispatcher) newAuthorizer() (security.Authorizer, error) {
	if d.internal.testMode {
		// In test mode, the device manager will not be able to read
		// the ACLs, because they were signed with the key of the real
		// device manager. It's not a problem because the
		// testModeDispatcher overrides the authorizer anyway.
		return nil, nil
	}
	rootTam, _, err := d.locks.GetPathACL(d.principal, aclDir(d.config))
	if err != nil {
		return nil, err
	}
	auth, err := access.TaggedACLAuthorizer(rootTam, access.TypicalTagType())
	if err != nil {
		vlog.Errorf("Successfully obtained an ACL from the filesystem but TaggedACLAuthorizer couldn't use it: %v", err)
		return nil, err
	}
	return auth, nil

}

// DISPATCHER INTERFACE IMPLEMENTATION
func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	components := strings.Split(suffix, "/")
	for i := 0; i < len(components); i++ {
		if len(components[i]) == 0 {
			components = append(components[:i], components[i+1:]...)
			i--
		}
	}

	auth, err := d.newAuthorizer()
	if err != nil {
		return nil, nil, err
	}

	if len(components) == 0 {
		return ipc.ChildrenGlobberInvoker(deviceSuffix, appsSuffix), auth, nil
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
				return logsimpl.NewLogFileService(logsDir, suffix), auth, nil
			case "pprof", "stats":
				info, err := loadInstanceInfo(appInstanceDir)
				if err != nil {
					return nil, nil, err
				}
				if !instanceStateIs(appInstanceDir, started) {
					return nil, nil, verror.New(ErrInvalidSuffix, nil)
				}
				var desc []ipc.InterfaceDesc
				switch kind {
				case "pprof":
					desc = pprof.PProfServer(nil).Describe__()
				case "stats":
					desc = stats.StatsServer(nil).Describe__()
				}
				suffix := naming.Join("__debug", naming.Join(components[4:]...))
				remote := naming.JoinAddressName(info.AppCycleMgrName, suffix)
				invoker := newProxyInvoker(remote, access.Debug, desc)
				return invoker, auth, nil
			}
		}
		receiver := device.ApplicationServer(&appService{
			callback:      d.internal.callback,
			config:        d.config,
			suffix:        components[1:],
			uat:           d.uat,
			locks:         d.locks,
			securityAgent: d.internal.securityAgent,
			mtAddress:     d.mtAddress,
			reap:          d.reap,
		})
		appSpecificAuthorizer, err := newAppSpecificAuthorizer(auth, d.config, components[1:])
		if err != nil {
			return nil, nil, err
		}
		return receiver, appSpecificAuthorizer, nil
	case configSuffix:
		if len(components) != 2 {
			return nil, nil, verror.New(ErrInvalidSuffix, nil)
		}
		receiver := idevice.ConfigServer(&configService{
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
	realDispatcher ipc.Dispatcher
}

func (d *testModeDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	obj, _, err := d.realDispatcher.Lookup(suffix)
	return obj, d, err
}

func (testModeDispatcher) Authorize(ctx security.Context) error {
	if ctx.Suffix() == deviceSuffix && ctx.Method() == "Stop" {
		vlog.Infof("testModeDispatcher.Authorize: Allow %q.%s()", ctx.Suffix(), ctx.Method())
		return nil
	}
	vlog.Infof("testModeDispatcher.Authorize: Reject %q.%s()", ctx.Suffix(), ctx.Method())
	return verror.New(ErrInvalidSuffix, nil)
}

func newAppSpecificAuthorizer(sec security.Authorizer, config *config.State, suffix []string) (security.Authorizer, error) {
	// TODO(rjkroege): This does not support <appname>.Start() to start all
	// instances. Correct this.

	// If we are attempting a method invocation against "apps/", we use the
	// device-manager wide ACL.
	if len(suffix) == 0 || len(suffix) == 1 {
		return sec, nil
	}
	// Otherwise, we require a per-installation and per-instance ACL file.
	if len(suffix) == 2 {
		p, err := installationDirCore(suffix, config.Root)
		if err != nil {
			vlog.Errorf("newAppSpecificAuthorizer failed: %v", err)
			return nil, err
		}
		return access.TaggedACLAuthorizerFromFile(path.Join(p, "acls", "data"), access.TypicalTagType())
	}
	if len(suffix) > 2 {
		p, err := instanceDir(config.Root, suffix[0:3])
		if err != nil {
			vlog.Errorf("newAppSpecificAuthorizer failed: %v", err)
			return nil, err
		}
		return access.TaggedACLAuthorizerFromFile(path.Join(p, "acls", "data"), access.TypicalTagType())
	}
	return nil, verror.New(ErrInvalidSuffix, nil)
}

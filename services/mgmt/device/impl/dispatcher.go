package impl

import (
	"crypto/subtle"
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

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/device"
	"v.io/core/veyron2/services/mgmt/pprof"
	"v.io/core/veyron2/services/mgmt/stats"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/vlog"
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
	// pairingToken (if present) is a token that the device manager expects
	// to be replayed by the principal who claims the device.
	pairingToken string
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

const (
	appsSuffix   = "apps"
	deviceSuffix = "device"
	configSuffix = "cfg"

	pkgPath = "v.io/core/veyron/services/mgmt/device/impl"
)

var (
	ErrInvalidSuffix       = verror.Register(pkgPath+".InvalidSuffix", verror.NoRetry, "{1:}{2:} invalid suffix{:_}")
	ErrOperationFailed     = verror.Register(pkgPath+".OperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
	ErrOperationInProgress = verror.Register(pkgPath+".OperationInProgress", verror.NoRetry, "{1:}{2:} operation in progress{:_}")
	ErrAppTitleMismatch    = verror.Register(pkgPath+".AppTitleMismatch", verror.NoRetry, "{1:}{2:} app title mismatch{:_}")
	ErrUpdateNoOp          = verror.Register(pkgPath+".UpdateNoOp", verror.NoRetry, "{1:}{2:} update is no op{:_}")
	ErrInvalidOperation    = verror.Register(pkgPath+".InvalidOperation", verror.NoRetry, "{1:}{2:} invalid operation{:_}")
	ErrInvalidBlessing     = verror.Register(pkgPath+".InvalidBlessing", verror.NoRetry, "{1:}{2:} invalid blessing{:_}")
	ErrInvalidPairingToken = verror.Register(pkgPath+".InvalidPairingToken", verror.NoRetry, "{1:}{2:} pairing token mismatch{:_}")
)

// NewDispatcher is the device manager dispatcher factory.
func NewDispatcher(ctx *context.T, config *config.State, mtAddress string, pairingToken string, testMode bool, restartHandler func()) (ipc.Dispatcher, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %v: %v", config, err)
	}
	// TODO(caprita): use some mechanism (a file lock or presence of entry
	// in mounttable) to ensure only one device manager is running in an
	// installation?
	mi := &managerInfo{
		MgrName: naming.Join(config.Name, deviceSuffix),
		Pid:     os.Getpid(),
	}
	if err := saveManagerInfo(filepath.Join(config.Root, "device-manager"), mi); err != nil {
		return nil, fmt.Errorf("failed to save info: %v", err)
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
		config:       config,
		uat:          uat,
		locks:        acls.NewLocks(),
		principal:    veyron2.GetPrincipal(ctx),
		mtAddress:    mtAddress,
		reap:         reap,
		pairingToken: pairingToken,
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

func (d *dispatcher) getACLDir() string {
	return filepath.Join(d.config.Root, "device-manager", "device-data", "acls")
}

func (d *dispatcher) claimDeviceManager(ctx ipc.ServerContext, pairingToken string) error {
	// TODO(rjkroege): Scrub the state tree of installation and instance ACL files.

	// Verify that the claimer pairing tokens match that of the device manager.
	if subtle.ConstantTimeCompare([]byte(pairingToken), []byte(d.pairingToken)) != 1 {
		return verror.New(ErrInvalidPairingToken, ctx.Context())
	}
	// Get the blessings to be used by the claimant.
	blessings := ctx.Blessings()
	if blessings == nil {
		return verror.New(ErrInvalidBlessing, ctx.Context())
	}
	principal := ctx.LocalPrincipal()
	if err := principal.AddToRoots(blessings); err != nil {
		vlog.Errorf("principal.AddToRoots(%s) failed: %v", blessings, err)
		return verror.New(ErrInvalidBlessing, ctx.Context())
	}
	names, err := blessings.ForContext(ctx)
	if len(names) == 0 {
		vlog.Errorf("No names for claimer(%v): %v", blessings, err)
		return verror.New(ErrOperationFailed, nil)
	}
	principal.BlessingStore().Set(blessings, security.AllPrincipals)
	principal.BlessingStore().SetDefault(blessings)
	// Create ACLs to transfer devicemanager permissions to the provided identity.
	acl := make(access.TaggedACLMap)
	for _, n := range names {
		// TODO(caprita, ataly, ashankar): Do we really need the NonExtendable restriction
		// below?
		patterns := security.BlessingPattern(n).MakeNonExtendable().PrefixPatterns()
		for _, p := range patterns {
			for _, tag := range access.AllTypicalTags() {
				acl.Add(p, string(tag))
			}
		}
	}
	if err := d.locks.SetPathACL(principal, d.getACLDir(), acl, ""); err != nil {
		vlog.Errorf("Failed to setACL:%v", err)
		return verror.New(ErrOperationFailed, nil)
	}
	return nil
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

	dir := d.getACLDir()
	rootTam, _, err := d.locks.GetPathACL(d.principal, dir)

	if err != nil && os.IsNotExist(err) {
		vlog.VI(1).Infof("GetPathACL(%s) failed: %v", dir, err)
		return allowEveryone{}, nil
	} else if err != nil {
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

// allowEveryone implements the authorization policy that allows all principals
// access.
type allowEveryone struct{}

func (allowEveryone) Authorize(ctx security.Context) error {
	vlog.VI(2).Infof("Device manager is unclaimed. Allow %q.%s() from %q.", ctx.Suffix(), ctx.Method(), ctx.RemoteBlessings())
	return nil
}

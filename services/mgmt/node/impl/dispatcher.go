package impl

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/agent"
	"veyron.io/veyron/veyron/security/agent/keymgr"
	vflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron/security/serialization"
	logsimpl "veyron.io/veyron/veyron/services/mgmt/logreader/impl"
	inode "veyron.io/veyron/veyron/services/mgmt/node"
	"veyron.io/veyron/veyron/services/mgmt/node/config"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/node"
	"veyron.io/veyron/veyron2/services/mgmt/pprof"
	"veyron.io/veyron/veyron2/services/mgmt/stats"
	"veyron.io/veyron/veyron2/services/security/access"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

// internalState wraps state shared between different node manager
// invocations.
type internalState struct {
	callback      *callbackState
	updating      *updatingState
	securityAgent *securityAgentState
}

// aclLocks provides a mutex lock for each acl file path.
type aclLocks map[string]*sync.Mutex

// dispatcher holds the state of the node manager dispatcher.
type dispatcher struct {
	// acl/auth hold the acl and authorizer used to authorize access to the
	// node manager methods.
	acl  security.ACL
	auth security.Authorizer
	// etag holds the version string for the ACL. We use this for optimistic
	// concurrency control when clients update the ACLs for the node manager.
	etag string
	// internal holds the state that persists across RPC method invocations.
	internal *internalState
	// config holds the node manager's (immutable) configuration state.
	config *config.State
	// dispatcherMutex is a lock for coordinating concurrent access to some
	// dispatcher methods.
	mu sync.RWMutex
	// TODO(rjkroege): Consider moving this inside internal.
	uat BlessingSystemAssociationStore
	// TODO(rjkroege): Eliminate need for locks.
	locks aclLocks
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

const (
	appsSuffix   = "apps"
	nodeSuffix   = "nm"
	configSuffix = "cfg"
)

var (
	errInvalidSuffix      = verror.BadArgf("invalid suffix")
	errOperationFailed    = verror.Internalf("operation failed")
	errInProgress         = verror.Existsf("operation in progress")
	errIncompatibleUpdate = verror.BadArgf("update failed: mismatching app title")
	// TODO(bprosnitz) Remove the TODO blocks in util_test when these are upgraded to verror2
	errUpdateNoOp       = verror.NoExistf("no different version available")
	errNotExist         = verror.NoExistf("object does not exist")
	errInvalidOperation = verror.BadArgf("invalid operation")
	errInvalidBlessing  = verror.BadArgf("invalid claim blessing")
)

// NewDispatcher is the node manager dispatcher factory.
func NewDispatcher(config *config.State) (*dispatcher, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %v: %v", config, err)
	}
	uat, err := NewBlessingSystemAssociationStore(config.Root)
	if err != nil {
		return nil, fmt.Errorf("cannot create persistent store for identity to system account associations: %v", err)
	}
	d := &dispatcher{
		etag: "default",
		internal: &internalState{
			callback: newCallbackState(config.Name),
			updating: newUpdatingState(),
		},
		config: config,
		uat:    uat,
		locks:  make(aclLocks),
	}
	// If there exists a signed ACL from a previous instance we prefer that.
	aclFile, sigFile, _ := d.getACLFilePaths()
	if _, err := os.Stat(aclFile); err == nil {
		perm := os.FileMode(0700)
		data, err := os.OpenFile(aclFile, os.O_RDONLY, perm)
		if err != nil {
			return nil, fmt.Errorf("failed to open acl file:%v", err)
		}
		defer data.Close()
		sig, err := os.OpenFile(sigFile, os.O_RDONLY, perm)
		if err != nil {
			return nil, fmt.Errorf("failed to open signature file:%v", err)
		}
		defer sig.Close()
		// read and verify the signature of the acl file
		reader, err := serialization.NewVerifyingReader(data, sig, rt.R().Principal().PublicKey())
		if err != nil {
			return nil, fmt.Errorf("failed to read nodemanager ACL file:%v", err)
		}
		acl, err := vsecurity.LoadACL(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to load nodemanager ACL:%v", err)
		}
		if err := d.setACL(acl, d.etag, false /* just update etag */); err != nil {
			return nil, err
		}
	} else {
		if d.auth = vflag.NewAuthorizerOrDie(); d.auth == nil {
			// If there are no specified ACLs we grant nodemanager access to all
			// principals until it is claimed.
			d.auth = vsecurity.NewACLAuthorizer(vsecurity.OpenACL())
		}
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
	return d, nil
}

func (d *dispatcher) getACLFilePaths() (acl, signature, nodedata string) {
	nodedata = filepath.Join(d.config.Root, "node-manager", "node-data")
	acl, signature = filepath.Join(nodedata, "acl.nodemanager"), filepath.Join(nodedata, "acl.signature")
	return
}

func (d *dispatcher) claimNodeManager(names []string, proof security.Blessings) error {
	// TODO(gauthamt): Should we start trusting these identity providers?
	// TODO(rjkroege): Scrub the state tree of installation and instance ACL files.
	if len(names) == 0 {
		vlog.Errorf("No names for claimer(%v) are trusted", proof)
		return errOperationFailed
	}
	rt.R().Principal().BlessingStore().Set(proof, security.AllPrincipals)
	rt.R().Principal().BlessingStore().SetDefault(proof)
	// Create ACLs to transfer nodemanager permissions to the provided identity.
	acl := security.ACL{In: make(map[security.BlessingPattern]security.LabelSet)}
	for _, name := range names {
		acl.In[security.BlessingPattern(name)] = security.AllLabels
	}
	_, etag, err := d.getACL()
	if err != nil {
		vlog.Errorf("Failed to getACL:%v", err)
		return errOperationFailed
	}
	if err := d.setACL(acl, etag, true /* store ACL on disk */); err != nil {
		vlog.Errorf("Failed to setACL:%v", err)
		return errOperationFailed
	}
	return nil
}

// TODO(rjkroege): Further refactor ACL-setting code.
func setAppACL(locks aclLocks, dir string, acl security.ACL, etag string) error {
	aclpath := path.Join(dir, "acls", "data")
	sigpath := path.Join(dir, "acls", "signature")

	// Acquire lock. Locks are per path to an acls file.
	lck, contains := locks[dir]
	if !contains {
		lck = new(sync.Mutex)
		locks[dir] = lck
	}
	lck.Lock()
	defer lck.Unlock()

	f, err := os.Open(aclpath)
	if err != nil {
		vlog.Errorf("LoadACL(%s) failed: %v", aclpath, err)
		return err
	}
	defer f.Close()

	curACL, err := vsecurity.LoadACL(f)
	if err != nil {
		vlog.Errorf("LoadACL(%s) failed: %v", aclpath, err)
		return err
	}
	curEtag, err := computeEtag(curACL)
	if err != nil {
		vlog.Errorf("computeEtag failed: %v", err)
		return err
	}

	if len(etag) > 0 && etag != curEtag {
		return verror.Make(access.ErrBadEtag, fmt.Sprintf("etag mismatch in:%s vers:%s", etag, curEtag))
	}

	return writeACLs(aclpath, sigpath, dir, acl)
}

func getAppACL(locks aclLocks, dir string) (security.ACL, string, error) {
	aclpath := path.Join(dir, "acls", "data")

	// Acquire lock. Locks are per path to an acls file.
	lck, contains := locks[dir]
	if !contains {
		lck = new(sync.Mutex)
		locks[dir] = lck
	}
	lck.Lock()
	defer lck.Unlock()

	f, err := os.Open(aclpath)
	if err != nil {
		vlog.Errorf("LoadACL(%s) failed: %v", aclpath, err)
		return security.ACL{}, "", err
	}
	defer f.Close()

	acl, err := vsecurity.LoadACL(f)
	if err != nil {
		vlog.Errorf("LoadACL(%s) failed: %v", aclpath, err)
		return security.ACL{}, "", err
	}
	curEtag, err := computeEtag(acl)
	if err != nil {
		return security.ACL{}, "", err
	}

	if err != nil {
		return security.ACL{}, "", err
	}
	return acl, curEtag, nil
}

func computeEtag(acl security.ACL) (string, error) {
	b := new(bytes.Buffer)
	if err := vsecurity.SaveACL(b, acl); err != nil {
		vlog.Errorf("Failed to save ACL:%v", err)
		return "", err
	}
	// Update the acl/etag/authorizer for this dispatcher
	md5hash := md5.Sum(b.Bytes())
	etag := hex.EncodeToString(md5hash[:])
	return etag, nil
}

func writeACLs(aclFile, sigFile, dir string, acl security.ACL) error {
	// Create dir directory if it does not exist
	os.MkdirAll(dir, os.FileMode(0700))
	// Save the object to temporary data and signature files, and then move
	// those files to the actual data and signature file.
	data, err := ioutil.TempFile(dir, "data")
	if err != nil {
		vlog.Errorf("Failed to open tmpfile data:%v", err)
		return errOperationFailed
	}
	defer os.Remove(data.Name())
	sig, err := ioutil.TempFile(dir, "sig")
	if err != nil {
		vlog.Errorf("Failed to open tmpfile sig:%v", err)
		return errOperationFailed
	}
	defer os.Remove(sig.Name())
	writer, err := serialization.NewSigningWriteCloser(data, sig, rt.R().Principal(), nil)
	if err != nil {
		vlog.Errorf("Failed to create NewSigningWriteCloser:%v", err)
		return errOperationFailed
	}
	if err = vsecurity.SaveACL(writer, acl); err != nil {
		vlog.Errorf("Failed to SaveACL:%v", err)
		return errOperationFailed
	}
	if err = writer.Close(); err != nil {
		vlog.Errorf("Failed to Close() SigningWriteCloser:%v", err)
		return errOperationFailed
	}
	if err := os.Rename(data.Name(), aclFile); err != nil {
		return err
	}
	if err := os.Rename(sig.Name(), sigFile); err != nil {
		return err
	}
	return nil
}

func (d *dispatcher) setACL(acl security.ACL, etag string, writeToFile bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	aclFile, sigFile, nodedata := d.getACLFilePaths()

	if len(etag) > 0 && etag != d.etag {
		return verror.Make(access.ErrBadEtag, fmt.Sprintf("etag mismatch in:%s vers:%s", etag, d.etag))
	}
	if writeToFile {
		if err := writeACLs(aclFile, sigFile, nodedata, acl); err != nil {
			return err
		}
	}

	etag, err := computeEtag(acl)
	if err != nil {
		return err
	}
	d.acl, d.etag, d.auth = acl, etag, vsecurity.NewACLAuthorizer(acl)
	return nil
}

func (d *dispatcher) getACL() (acl security.ACL, etag string, err error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.acl, d.etag, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION
func (d *dispatcher) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	components := strings.Split(suffix, "/")
	for i := 0; i < len(components); i++ {
		if len(components[i]) == 0 {
			components = append(components[:i], components[i+1:]...)
			i--
		}
	}
	if len(components) == 0 {
		if method == ipc.GlobMethod {
			return ipc.VChildrenGlobberInvoker(nodeSuffix, appsSuffix), d.auth, nil
		}
		return nil, nil, errInvalidSuffix
	}
	// The implementation of the node manager is split up into several
	// invokers, which are instantiated depending on the receiver name
	// prefix.
	switch components[0] {
	case nodeSuffix:
		receiver := node.NodeServer(&nodeService{
			callback: d.internal.callback,
			updating: d.internal.updating,
			config:   d.config,
			disp:     d,
			uat:      d.uat,
		})
		return receiver, d.auth, nil
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
				return logsimpl.NewLogFileService(logsDir, suffix), d.auth, nil
			case "pprof", "stats":
				info, err := loadInstanceInfo(appInstanceDir)
				if err != nil {
					return nil, nil, err
				}
				if !instanceStateIs(appInstanceDir, started) {
					return nil, nil, errInvalidSuffix
				}
				var label security.Label
				var sigStub signatureStub
				if kind == "pprof" {
					label = security.DebugLabel
					sigStub = pprof.PProfServer(nil)
				} else {
					label = security.DebugLabel | security.MonitoringLabel
					sigStub = stats.StatsServer(nil)
				}
				suffix := naming.Join("__debug", naming.Join(components[4:]...))
				remote := naming.JoinAddressName(info.AppCycleMgrName, suffix)
				return &proxyInvoker{remote, label, sigStub}, d.auth, nil
			}
		}
		nodeACLs, _, err := d.getACL()
		if err != nil {
			return nil, nil, err
		}
		receiver := node.ApplicationServer(&appService{
			callback:      d.internal.callback,
			config:        d.config,
			suffix:        components[1:],
			uat:           d.uat,
			locks:         d.locks,
			nodeACL:       nodeACLs,
			securityAgent: d.internal.securityAgent,
		})
		appSpecificAuthorizer, err := newAppSpecificAuthorizer(d.auth, d.config, components[1:])
		if err != nil {
			return nil, nil, err
		}
		return receiver, appSpecificAuthorizer, nil
	case configSuffix:
		if len(components) != 2 {
			return nil, nil, errInvalidSuffix
		}
		receiver := inode.ConfigServer(&configService{
			callback: d.internal.callback,
			suffix:   components[1],
		})
		// The nil authorizer ensures that only principals blessed by
		// the node manager can talk back to it.  All apps started by
		// the node manager should fall in that category.
		//
		// TODO(caprita,rjkroege): We should further refine this, by
		// only allowing the app to update state referring to itself
		// (and not other apps).
		return receiver, nil, nil
	default:
		return nil, nil, errInvalidSuffix
	}
}

func newAppSpecificAuthorizer(sec security.Authorizer, config *config.State, suffix []string) (security.Authorizer, error) {
	// TODO(rjkroege): This does not support <appname>.Start() to start all instances. Correct this.

	// If we are attempting a method invocation against "apps/", we use the node-manager wide ACL.
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
		p = path.Join(p, "acls", "data")
		return vsecurity.NewFileACLAuthorizer(p), nil
	} else if len(suffix) > 2 {
		p, err := instanceDir(config.Root, suffix[0:3])
		if err != nil {
			vlog.Errorf("newAppSpecificAuthorizer failed: %v", err)
			return nil, err
		}
		p = path.Join(p, "acls", "data")
		return vsecurity.NewFileACLAuthorizer(p), nil
	}
	return nil, errInvalidSuffix
}

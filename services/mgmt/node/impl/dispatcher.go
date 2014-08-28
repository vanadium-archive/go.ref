package impl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	vsecurity "veyron/security"
	vflag "veyron/security/flag"
	"veyron/security/serialization"
	inode "veyron/services/mgmt/node"
	"veyron/services/mgmt/node/config"

	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mgmt/node"
	"veyron2/verror"
	"veyron2/vlog"
)

// internalState wraps state shared between different node manager
// invocations.
type internalState struct {
	callback *callbackState
	updating *updatingState
}

// dispatcher holds the state of the node manager dispatcher.
type dispatcher struct {
	auth security.Authorizer
	// internal holds the state that persists across RPC method invocations.
	internal *internalState
	// config holds the node manager's (immutable) configuration state.
	config *config.State
}

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
	errUpdateNoOp         = verror.NotFoundf("no different version available")
	errNotExist           = verror.NotFoundf("object does not exist")
	errInvalidOperation   = verror.BadArgf("invalid operation")
	errInvalidBlessing    = verror.BadArgf("invalid claim blessing")
)

// NewDispatcher is the node manager dispatcher factory.
func NewDispatcher(config *config.State) (*dispatcher, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("Invalid config %v: %v", config, err)
	}
	d := &dispatcher{
		internal: &internalState{
			callback: newCallbackState(config.Name),
			updating: newUpdatingState(),
		},
		config: config,
	}
	// Prefer ACLs in the nodemanager data directory if they exist.
	if data, sig, err := d.getACLFiles(os.O_RDONLY); err != nil {
		if d.auth = vflag.NewAuthorizerOrDie(); d.auth == nil {
			// If there are no specified ACLs we grant nodemanager access to all
			// principal until it is claimed.
			d.auth = vsecurity.NewACLAuthorizer(vsecurity.OpenACL())
		}
	} else {
		defer data.Close()
		defer sig.Close()
		reader, err := serialization.NewVerifyingReader(data, sig, rt.R().Identity().PublicKey())
		if err != nil {
			return nil, fmt.Errorf("Failed to read nodemanager ACL file:%v", err)
		}
		acl, err := vsecurity.LoadACL(reader)
		if err != nil {
			return nil, fmt.Errorf("Failed to load nodemanager ACL:%v", err)
		}
		d.auth = vsecurity.NewACLAuthorizer(acl)
	}
	return d, nil
}

func (d *dispatcher) getACLFiles(flag int) (aclData *os.File, aclSig *os.File, err error) {
	nodedata := filepath.Join(d.config.Root, "node-manager", "node-data")
	perm := os.FileMode(0700)
	if err = os.MkdirAll(nodedata, perm); err != nil {
		return
	}
	if aclData, err = os.OpenFile(filepath.Join(nodedata, "acl.nodemanager"), flag, perm); err != nil {
		return
	}
	if aclSig, err = os.OpenFile(filepath.Join(nodedata, "acl.signature"), flag, perm); err != nil {
		return
	}
	return
}

func (d *dispatcher) claimNodeManager(id security.PublicID) error {
	// TODO(gauthamt): Should we start trusting these identity providers?
	if id.Names() == nil {
		vlog.Errorf("Identity provider for device claimer is not trusted")
		return errOperationFailed
	}
	rt.R().PublicIDStore().Add(id, security.AllPrincipals)
	// Create ACLs to transfer nodemanager permissions to the provided identity.
	acl := security.ACL{In: make(map[security.BlessingPattern]security.LabelSet)}
	for _, name := range id.Names() {
		acl.In[security.BlessingPattern(name)] = security.AllLabels
	}
	d.auth = vsecurity.NewACLAuthorizer(acl)
	// Write out the ACLs so that it will persist across restarts.
	data, sig, err := d.getACLFiles(os.O_CREATE | os.O_RDWR)
	if err != nil {
		vlog.Errorf("Failed to create ACL files:%v", err)
		return errOperationFailed
	}
	writer, err := serialization.NewSigningWriteCloser(data, sig, rt.R().Identity(), nil)
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
	return nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	components := strings.Split(suffix, "/")
	for i := 0; i < len(components); i++ {
		if len(components[i]) == 0 {
			components = append(components[:i], components[i+1:]...)
			i--
		}
	}
	if len(components) == 0 {
		return nil, nil, errInvalidSuffix
	}
	// The implementation of the node manager is split up into several
	// invokers, which are instantiated depending on the receiver name
	// prefix.
	var receiver interface{}
	switch components[0] {
	case nodeSuffix:
		receiver = node.NewServerNode(&nodeInvoker{
			callback: d.internal.callback,
			updating: d.internal.updating,
			config:   d.config,
			disp:     d,
		})
	case appsSuffix:
		receiver = node.NewServerApplication(&appInvoker{
			callback: d.internal.callback,
			config:   d.config,
			suffix:   components[1:],
		})
	case configSuffix:
		if len(components) != 2 {
			return nil, nil, errInvalidSuffix
		}
		receiver = inode.NewServerConfig(&configInvoker{
			callback: d.internal.callback,
			suffix:   components[1],
		})
	default:
		return nil, nil, errInvalidSuffix
	}
	return ipc.ReflectInvoker(receiver), d.auth, nil
}

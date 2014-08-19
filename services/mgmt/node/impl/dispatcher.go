package impl

import (
	"fmt"
	"strings"

	inode "veyron/services/mgmt/node"
	"veyron/services/mgmt/node/config"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/mgmt/node"
	"veyron2/verror"
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
)

// NewDispatcher is the node manager dispatcher factory.
func NewDispatcher(auth security.Authorizer, config *config.State) (*dispatcher, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("Invalid config %v: %v", config, err)
	}
	return &dispatcher{
		auth: auth,
		internal: &internalState{
			callback: newCallbackState(),
			updating: newUpdatingState(),
		},
		config: config,
	}, nil
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
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

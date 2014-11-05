package impl

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/repository"
)

const (
	VersionFile = "VERSION"
	Version     = "1.0"
)

// dispatcher holds the state of the binary repository dispatcher.
type dispatcher struct {
	auth  security.Authorizer
	state *state
}

// TODO(caprita): Move this together with state into a new file, state.go.

// NewState creates a new state object for the binary service.  This
// should be passed into both NewDispatcher and NewHTTPRoot.
func NewState(root string, depth int) (*state, error) {
	if min, max := 0, md5.Size-1; min > depth || depth > max {
		return nil, fmt.Errorf("Unexpected depth, expected a value between %v and %v, got %v", min, max, depth)
	}
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("Stat(%v) failed: %v", root, err)
	}
	path := filepath.Join(root, VersionFile)
	output, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ReadFile(%v) failed: %v", path, err)
	}
	if expected, got := Version, strings.TrimSpace(string(output)); expected != got {
		return nil, fmt.Errorf("Unexpected version: expected %v, got %v", expected, got)
	}
	return &state{
		depth: depth,
		root:  root,
	}, nil
}

// NewDispatcher is the dispatcher factory.
func NewDispatcher(state *state, authorizer security.Authorizer) ipc.Dispatcher {
	return &dispatcher{
		auth:  authorizer,
		state: state,
	}
}

// DISPATCHER INTERFACE IMPLEMENTATION

func (d *dispatcher) Lookup(suffix, method string) (interface{}, security.Authorizer, error) {
	invoker := ipc.ReflectInvoker(repository.NewServerBinary(newInvoker(d.state, suffix)))
	return invoker, d.auth, nil
}

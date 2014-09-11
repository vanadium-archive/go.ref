// Package debug implements a server that exports debugging information from
// various sources: log files, stats, etc.
package debug

import (
	"fmt"

	"veyron2"
	"veyron2/security"
	"veyron2/vlog"
)

// StartDebugServer starts a debug server.
func StartDebugServer(rt veyron2.Runtime, address, logsDir string, auth security.Authorizer) (string, func(), error) {
	if len(logsDir) == 0 {
		return "", nil, fmt.Errorf("logs directory missing")
	}
	disp := NewDispatcher(logsDir, auth)
	server, err := rt.NewServer()
	if err != nil {
		return "", nil, fmt.Errorf("failed to start debug server: %v", err)
	}
	endpoint, err := server.Listen("tcp", address)
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen on %v: %v", address, err)
	}
	if err := server.Serve("", disp); err != nil {
		return "", nil, err
	}
	ep := endpoint.String()
	vlog.VI(1).Infof("Debug server listening on %v", ep)
	return ep, func() { server.Stop() }, nil
}

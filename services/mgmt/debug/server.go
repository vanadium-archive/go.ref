// Package debug implements a server that exports debugging information from
// various sources: log files, stats, etc.
package debug

import (
	"fmt"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

// StartDebugServer starts a debug server.
func StartDebugServer(rt veyron2.Runtime, listenSpec ipc.ListenSpec, logsDir string, auth security.Authorizer) (string, func(), error) {
	if len(logsDir) == 0 {
		return "", nil, fmt.Errorf("logs directory missing")
	}
	disp := NewDispatcher(logsDir, auth)
	server, err := rt.NewServer()
	if err != nil {
		return "", nil, fmt.Errorf("failed to start debug server: %v", err)
	}
	endpoint, err := server.Listen(listenSpec)
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen on %s: %v", listenSpec, err)
	}
	if err := server.ServeDispatcher("", disp); err != nil {
		return "", nil, err
	}
	ep := endpoint.String()
	vlog.VI(1).Infof("Debug server listening on %v", ep)
	return ep, func() { server.Stop() }, nil
}

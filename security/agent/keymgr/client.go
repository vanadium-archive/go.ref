// Package keymgr provides a client for the Node Manager to manage keys in
// the "Agent" process.
package keymgr

import (
	"net"
	"os"
	"strconv"
	"sync"

	"veyron.io/veyron/veyron/lib/unixfd"
	"veyron.io/veyron/veyron/security/agent/server"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/verror"
)

const defaultManagerSocket = 4

type Agent struct {
	conn *net.UnixConn // Guarded by mu
	mu   sync.Mutex
}

// NewAgent returns a client connected to the agent on the default file descriptors.
func NewAgent() (*Agent, error) {
	return newAgent(defaultManagerSocket)
}

func newAgent(fd int) (a *Agent, err error) {
	file := os.NewFile(uintptr(fd), "fd")
	defer file.Close()
	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}

	return &Agent{conn: conn.(*net.UnixConn)}, nil
}

// TODO(caprita): Get rid of context.T arg.  Doesn't seem to be used.

// NewPrincipal creates a new principal and returns the handle and a socket serving
// the principal.
// Typically the socket will be passed to a child process using cmd.ExtraFiles.
func (a *Agent) NewPrincipal(_ context.T, inMemory bool) (handle []byte, conn *os.File, err error) {
	req := make([]byte, 1)
	if inMemory {
		req[0] = 1
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	conn, err = a.connect(req)
	if err != nil {
		return nil, nil, err
	}
	buf := make([]byte, server.PrincipalHandleByteSize)
	n, err := a.conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if n != server.PrincipalHandleByteSize {
		conn.Close()
		return nil, nil, verror.BadProtocolf("invalid response from agent. (expected %d bytes, got %d)", server.PrincipalHandleByteSize, n)
	}
	return buf, conn, nil
}

func (a *Agent) connect(req []byte) (*os.File, error) {
	// We're passing this to a child, so no CLOEXEC.
	addr, err := unixfd.SendConnection(a.conn, req, false)
	if err != nil {
		return nil, err
	}
	fd, err := strconv.ParseInt(addr.String(), 10, 32)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "client"), nil
}

// NewConnection creates a connection to an agent which exports a principal
// previously created with NewPrincipal.
// Typically this will be passed to a child process using cmd.ExtraFiles.
func (a *Agent) NewConnection(handle []byte) (*os.File, error) {
	if len(handle) != server.PrincipalHandleByteSize {
		return nil, verror.BadArgf("Invalid key handle")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connect(handle)
}

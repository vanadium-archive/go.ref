// Package keymgr provides a client for the Device Manager to manage keys in
// the "Agent" process.
package keymgr

import (
	"net"
	"os"
	"strconv"
	"sync"

	"v.io/veyron/veyron/lib/unixfd"
	"v.io/veyron/veyron/security/agent/server"
	"v.io/veyron/veyron2/context"
	verror "v.io/veyron/veyron2/verror2"
)

const pkgPath = "v.io/veyron/veyron/security/agent/keymgr"

// Errors
var (
	errInvalidResponse = verror.Register(pkgPath+".errInvalidResponse",
		verror.NoRetry, "{1:}{2:} invalid response from agent. (expected {3} bytes, got {4})")
	errInvalidKeyHandle = verror.Register(pkgPath+".errInvalidKeyHandle",
		verror.NoRetry, "{1:}{2:} Invalid key handle")
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
func (a *Agent) NewPrincipal(ctx context.T, inMemory bool) (handle []byte, conn *os.File, err error) {
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
		return nil, nil, verror.Make(errInvalidResponse, ctx, server.PrincipalHandleByteSize, n)
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
		return nil, verror.Make(errInvalidKeyHandle, nil)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connect(handle)
}

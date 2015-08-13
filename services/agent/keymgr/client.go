// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package keymgr implements a client for deviced to manage keys in the agentd
// process.
package keymgr

import (
	"net"
	"os"
	"strconv"
	"sync"

	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/internal/ipc"
	"v.io/x/ref/services/agent/internal/server"
	"v.io/x/ref/services/agent/internal/unixfd"
)

const pkgPath = "v.io/x/ref/services/agent/keymgr"

// Errors
var (
	errInvalidResponse = verror.Register(pkgPath+".errInvalidResponse",
		verror.NoRetry, "{1:}{2:} invalid response from agent. (expected {3} bytes, got {4})")
	errInvalidKeyHandle = verror.Register(pkgPath+".errInvalidKeyHandle",
		verror.NoRetry, "{1:}{2:} Invalid key handle")
)

const defaultManagerSocket = 4

type KeyManager struct {
	conn *ipc.IPCConn
}

type Agent struct {
	conn *net.UnixConn // Guarded by mu
	mu   sync.Mutex
}

// NewAgent returns a client connected to the agent on the default file descriptors.
func NewAgent() (*Agent, error) {
	return newAgent(defaultManagerSocket)
}

// NewKeyManager returns a client connected to the specified KeyManager.
func NewKeyManager(path string) (agent.KeyManager, error) {
	i := ipc.NewIPC()
	conn, err := i.Connect(path)
	var m *KeyManager
	if err == nil {
		m = &KeyManager{conn}
	}
	return m, err
}

func NewLocalAgent(ctx *context.T, path string, passphrase []byte) (*Agent, error) {
	file, err := server.RunKeyManager(ctx, path, passphrase)
	if err != nil {
		return nil, err
	}
	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}
	return &Agent{conn: conn.(*net.UnixConn)}, nil
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

// TODO(caprita): Get rid of *context.T arg.  Doesn't seem to be used.

// NewPrincipal creates a new principal and returns the handle and a socket serving
// the principal.
// Typically the socket will be passed to a child process using cmd.ExtraFiles.
func (a *Agent) NewPrincipal(ctx *context.T, inMemory bool) (handle []byte, conn *os.File, err error) {
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
	buf := make([]byte, agent.PrincipalHandleByteSize)
	n, err := a.conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if n != agent.PrincipalHandleByteSize {
		conn.Close()
		return nil, nil, verror.New(errInvalidResponse, ctx, agent.PrincipalHandleByteSize, n)
	}
	return buf, conn, nil
}

// NewPrincipal creates a new principal and returns a handle.
// The handle may be passed to ServePrincipal to start an agent serving the principal.
func (m *KeyManager) NewPrincipal(inMemory bool) (handle [agent.PrincipalHandleByteSize]byte, err error) {
	args := []interface{}{inMemory}
	err = m.conn.Call("NewPrincipal", args, &handle)
	return
}

func (a *Agent) connect(req []byte) (*os.File, error) {
	addr, err := unixfd.SendConnection(a.conn, req)
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
	if len(handle) != agent.PrincipalHandleByteSize {
		return nil, verror.New(errInvalidKeyHandle, nil)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connect(handle)
}

// ServePrincipal creates a socket at socketPath and serves a principal
// previously created with NewPrincipal.
func (m *KeyManager) ServePrincipal(handle [agent.PrincipalHandleByteSize]byte, socketPath string) error {
	args := []interface{}{handle, socketPath}
	return m.conn.Call("ServePrincipal", args)
}

// StopServing shuts down a server previously started with ServePrincipal.
// The principal is not deleted and the server can be restarted by calling
// ServePrincipal again.
func (m *KeyManager) StopServing(handle [agent.PrincipalHandleByteSize]byte) error {
	args := []interface{}{handle}
	return m.conn.Call("StopServing", args)
}

// DeletePrincipal shuts down a server started by ServePrincipal and additionally
// deletes the principal.
func (m *KeyManager) DeletePrincipal(handle [agent.PrincipalHandleByteSize]byte) error {
	args := []interface{}{handle}
	return m.conn.Call("DeletePrincipal", args)
}

func (m *KeyManager) Close() error {
	m.conn.Close()
	return nil
}

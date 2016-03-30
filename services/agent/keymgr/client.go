// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package keymgr implements a client for deviced to manage keys in the agentd
// process.
package keymgr

import (
	"v.io/v23/verror"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/internal/ipc"
	"v.io/x/ref/services/agent/internal/server"
)

const pkgPath = "v.io/x/ref/services/agent/keymgr"

// Errors
var (
	errInvalidResponse = verror.Register(pkgPath+".errInvalidResponse",
		verror.NoRetry, "{1:}{2:} invalid response from agent. (expected {3} bytes, got {4})")
	errInvalidKeyHandle = verror.Register(pkgPath+".errInvalidKeyHandle",
		verror.NoRetry, "{1:}{2:} Invalid key handle")
)

type keyManager struct {
	conn *ipc.IPCConn
}

// NewKeyManager returns a client connected to the specified KeyManager.
func NewKeyManager(path string) (agent.KeyManager, error) {
	i := ipc.NewIPC()
	conn, err := i.Connect(path, 0)
	var m *keyManager
	if err == nil {
		m = &keyManager{conn}
	}
	return m, err
}

func NewLocalAgent(path string, passphrase []byte) (agent.KeyManager, error) {
	return server.NewLocalKeyManager(path, passphrase)
}

// NewPrincipal creates a new principal and returns a handle.
// The handle may be passed to ServePrincipal to start an agent serving the principal.
func (m *keyManager) NewPrincipal(inMemory bool) (handle [agent.PrincipalHandleByteSize]byte, err error) {
	args := []interface{}{inMemory}
	err = m.conn.Call("NewPrincipal", args, &handle)
	return
}

// ServePrincipal creates a socket at socketPath and serves a principal
// previously created with NewPrincipal.
func (m *keyManager) ServePrincipal(handle [agent.PrincipalHandleByteSize]byte, socketPath string) error {
	args := []interface{}{handle, socketPath}
	return m.conn.Call("ServePrincipal", args)
}

// StopServing shuts down a server previously started with ServePrincipal.
// The principal is not deleted and the server can be restarted by calling
// ServePrincipal again.
func (m *keyManager) StopServing(handle [agent.PrincipalHandleByteSize]byte) error {
	args := []interface{}{handle}
	return m.conn.Call("StopServing", args)
}

// DeletePrincipal shuts down a server started by ServePrincipal and additionally
// deletes the principal.
func (m *keyManager) DeletePrincipal(handle [agent.PrincipalHandleByteSize]byte) error {
	args := []interface{}{handle}
	return m.conn.Call("DeletePrincipal", args)
}

func (m *keyManager) Close() error {
	m.conn.Close()
	return nil
}

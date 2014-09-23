// Package agent provides a client for communicating with an "Agent"
// process holding the private key for an identity.
package agent

import (
	"fmt"
	"net"
	"os"

	"veyron.io/veyron/veyron/lib/unixfd"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/security/wire"
)

// FdVarName is the name of the environment variable containing
// the file descriptor for talking to the agent.
const FdVarName = "VEYRON_AGENT_FD"

type client struct {
	client ipc.Client
	name   string
	ctx    context.T
	key    security.PublicKey
}

func (c *client) call(name string, result interface{}, args ...interface{}) (err error) {
	var call ipc.Call
	if call, err = c.client.StartCall(c.ctx, c.name, name, args); err == nil {
		if ierr := call.Finish(result, &err); ierr != nil {
			err = ierr
		}
	}
	return
}

// NewAgentSigner returns a Signer using the PrivateKey held in a remote agent process.
// 'fd' is the socket for connecting to the agent, typically obtained from
// os.GetEnv(agent.FdVarName).
// 'ctx' should not have a deadline, and should never be cancelled.
func NewAgentSigner(c ipc.Client, fd int, ctx context.T) (security.Signer, error) {
	conn, err := net.FileConn(os.NewFile(uintptr(fd), "agent_client"))
	if err != nil {
		return nil, err
	}
	// This is just an arbitrary 1 byte string. The value is ignored.
	data := make([]byte, 1)
	addr, err := unixfd.SendConnection(conn.(*net.UnixConn), data)
	if err != nil {
		return nil, err
	}
	agent := &client{c, naming.JoinAddressName(naming.FormatEndpoint(addr.Network(), addr.String()), ""), ctx, nil}
	if err := agent.fetchPublicKey(); err != nil {
		return nil, err
	}
	return agent, nil
}

func (c *client) fetchPublicKey() (err error) {
	var key wire.PublicKey
	if err = c.call("PublicKey", &key); err != nil {
		return
	}
	c.key, err = key.Decode()
	return
}

func (c *client) PublicKey() security.PublicKey {
	return c.key
}

func (c *client) Sign(purpose, message []byte) (sig security.Signature, err error) {
	if purpose != nil {
		err = fmt.Errorf("purpose not supported")
		return
	}
	err = c.call("Sign", &sig, message)
	return
}

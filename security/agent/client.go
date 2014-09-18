// Package agent provides a client for communicating with an "Agent"
// process holding the private key for an identity.
package agent

import (
	"fmt"
	"strconv"

	"veyron.io/veyron/veyron/lib/unixfd"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/security/wire"
)

// EndpointVarName is the name of the environment variable containing
// the endpoint for talking to the agent.
const EndpointVarName = "VEYRON_AGENT_ENDPOINT"

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
// 'ctx' should not have a deadline, and should never be cancelled.
func NewAgentSigner(c ipc.Client, endpoint string, ctx context.T) (security.Signer, error) {
	agent := &client{c, naming.JoinAddressName(endpoint, ""), ctx, nil}
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

func CreateAgentEndpoint(fd int) string {
	return naming.FormatEndpoint(unixfd.Network, strconv.Itoa(fd))
}

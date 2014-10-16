// Package server provides a server which keeps a private key in memory
// and allows clients to use the key for signing.
//
// PROTOCOL
//
// The agent starts processes with the VEYRON_AGENT_FD set to one end of a
// unix domain socket. To connect to the agent, a client should create
// a unix domain socket pair. Then send one end of the socket to the agent
// with 1 byte of data. The agent will then serve the Agent service on
// the recieved socket, using VCSecurityNone.
package server

import (
	"io"
	"os"
	"veyron.io/veyron/veyron/lib/unixfd"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/security/wire"
	"veyron.io/veyron/veyron2/vlog"
)

type Signer interface {
	Sign(message []byte) (security.Signature, error)
	PublicKey() security.PublicKey
}

type agentd struct {
	signer Signer
}

// RunAnonymousAgent starts the agent server listening on an
// anonymous unix domain socket. It will respond to SignatureRequests
// using 'signer'.
// The returned 'client' is typically passed via cmd.ExtraFiles to a child process.
func RunAnonymousAgent(runtime veyron2.Runtime, signer Signer) (client *os.File, err error) {
	// VCSecurityNone is safe since we're using anonymous unix sockets.
	// Only our child process can possibly communicate on the socket.
	s, err := runtime.NewServer(options.VCSecurityNone)
	if err != nil {
		return nil, err
	}

	local, remote, err := unixfd.Socketpair()
	if err != nil {
		return nil, err
	}

	serverAgent := NewServerAgent(agentd{signer})
	go func() {
		buf := make([]byte, 1)
		for {
			clientAddr, _, err := unixfd.ReadConnection(local, buf)
			if err == io.EOF {
				return
			}
			if err == nil {
				_, err = s.Listen(clientAddr.Network(), clientAddr.String())
			}
			if err != nil {
				vlog.Infof("Error accepting connection: %v", err)
			}
		}
	}()
	if err = s.Serve("", ipc.LeafDispatcher(serverAgent, nil)); err != nil {
		return
	}
	return remote, nil
}

func (a agentd) Sign(_ ipc.ServerContext, message []byte) (security.Signature, error) {
	return a.signer.Sign(message)
}

func (a agentd) PublicKey(ipc.ServerContext) (key wire.PublicKey, err error) {
	err = key.Encode(a.signer.PublicKey())
	return
}

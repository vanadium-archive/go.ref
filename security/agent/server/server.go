// Package server provides a server which keeps a private key in memory
// and allows clients to use the key for signing.
package server

import (
	"os"
	"veyron.io/veyron/veyron/lib/unixfd"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/security/wire"
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
	s, err := runtime.NewServer(veyron2.VCSecurityNone)
	if err != nil {
		return nil, err
	}

	socks, err := unixfd.Socketpair()
	server_sock := socks[0]
	client_sock := make(chan *os.File, 1)
	client_sock <- socks[1]
	close(client_sock)
	defer server_sock.Close()
	defer func() {
		if sock, ok := <-client_sock; ok {
			sock.Close()
		}
	}()

	serverAgent := NewServerAgent(agentd{signer})
	addr := unixfd.Addr(server_sock.Fd())
	if _, err = s.Listen(addr.Network(), addr.String()); err != nil {
		return
	}
	if err = s.Serve("", ipc.LeafDispatcher(serverAgent, nil)); err != nil {
		return
	}
	return <-client_sock, nil
}

func (a agentd) Sign(_ ipc.ServerContext, message []byte) (security.Signature, error) {
	return a.signer.Sign(message)
}

func (a agentd) PublicKey(ipc.ServerContext) (key wire.PublicKey, err error) {
	err = key.Encode(a.signer.PublicKey())
	return
}

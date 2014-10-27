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
	"fmt"
	"io"
	"os"

	"veyron.io/veyron/veyron/lib/unixfd"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/vlog"
)

type agentd struct {
	principal security.Principal
}

// RunAnonymousAgent starts the agent server listening on an
// anonymous unix domain socket. It will respond to SignatureRequests
// using 'principal'.
// The returned 'client' is typically passed via cmd.ExtraFiles to a child process.
func RunAnonymousAgent(runtime veyron2.Runtime, principal security.Principal) (client *os.File, err error) {
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

	serverAgent := NewServerAgent(agentd{principal})
	go func() {
		buf := make([]byte, 1)
		for {
			clientAddr, _, err := unixfd.ReadConnection(local, buf)
			if err == io.EOF {
				return
			}
			if err == nil {
				spec := ipc.ListenSpec{Protocol: clientAddr.Network(), Address: clientAddr.String()}
				_, err = s.Listen(spec)
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

func (a agentd) Bless(_ ipc.ServerContext, key []byte, with security.WireBlessings, extension string, caveat security.Caveat, additionalCaveats []security.Caveat) (security.WireBlessings, error) {
	pkey, err := security.UnmarshalPublicKey(key)
	if err != nil {
		return security.WireBlessings{}, err
	}
	withBlessings, err := security.NewBlessings(with)
	if err != nil {
		return security.WireBlessings{}, err
	}
	blessings, err := a.principal.Bless(pkey, withBlessings, extension, caveat, additionalCaveats...)
	if err != nil {
		return security.WireBlessings{}, err
	}
	return security.MarshalBlessings(blessings), nil
}

func (a agentd) BlessSelf(_ ipc.ServerContext, name string, caveats []security.Caveat) (security.WireBlessings, error) {
	blessings, err := a.principal.BlessSelf(name, caveats...)
	if err != nil {
		return security.WireBlessings{}, err
	}
	return security.MarshalBlessings(blessings), nil
}

func (a agentd) Sign(_ ipc.ServerContext, message []byte) (security.Signature, error) {
	return a.principal.Sign(message)
}

func (a agentd) MintDischarge(_ ipc.ServerContext, tp vdlutil.Any, caveat security.Caveat, additionalCaveats []security.Caveat) (vdlutil.Any, error) {
	tpCaveat, ok := tp.(security.ThirdPartyCaveat)
	if !ok {
		return nil, fmt.Errorf("provided caveat of type %T does not implement security.ThirdPartyCaveat", tp)
	}
	return a.principal.MintDischarge(tpCaveat, caveat, additionalCaveats...)
}

func (a agentd) PublicKey(_ ipc.ServerContext) ([]byte, error) {
	return a.principal.PublicKey().MarshalBinary()
}

func (a agentd) AddToRoots(_ ipc.ServerContext, wireBlessings security.WireBlessings) error {
	blessings, err := security.NewBlessings(wireBlessings)
	if err != nil {
		return err
	}
	return a.principal.AddToRoots(blessings)
}

func (a agentd) BlessingStoreSet(_ ipc.ServerContext, wireBlessings security.WireBlessings, forPeers security.BlessingPattern) (security.WireBlessings, error) {
	blessings, err := security.NewBlessings(wireBlessings)
	if err != nil {
		return security.WireBlessings{}, err
	}
	resultBlessings, err := a.principal.BlessingStore().Set(blessings, forPeers)
	if err != nil {
		return security.WireBlessings{}, err
	}
	return security.MarshalBlessings(resultBlessings), nil
}

func (a agentd) BlessingStoreForPeer(_ ipc.ServerContext, peerBlessings []string) (security.WireBlessings, error) {
	return security.MarshalBlessings(a.principal.BlessingStore().ForPeer(peerBlessings...)), nil
}

func (a agentd) BlessingStoreSetDefault(_ ipc.ServerContext, wireBlessings security.WireBlessings) error {
	blessings, err := security.NewBlessings(wireBlessings)
	if err != nil {
		return err
	}
	return a.principal.BlessingStore().SetDefault(blessings)
}

func (a agentd) BlessingStoreDebugString(_ ipc.ServerContext) (string, error) {
	return a.principal.BlessingStore().DebugString(), nil
}

func (a agentd) BlessingStoreDefault(_ ipc.ServerContext) (security.WireBlessings, error) {
	return security.MarshalBlessings(a.principal.BlessingStore().Default()), nil
}

func (a agentd) BlessingRootsAdd(_ ipc.ServerContext, root []byte, pattern security.BlessingPattern) error {
	pkey, err := security.UnmarshalPublicKey(root)
	if err != nil {
		return err
	}
	return a.principal.Roots().Add(pkey, pattern)
}

func (a agentd) BlessingRootsRecognized(_ ipc.ServerContext, root []byte, blessing string) error {
	pkey, err := security.UnmarshalPublicKey(root)
	if err != nil {
		return err
	}
	return a.principal.Roots().Recognized(pkey, blessing)
}

func (a agentd) BlessingRootsDebugString(_ ipc.ServerContext) (string, error) {
	return a.principal.Roots().DebugString(), nil
}

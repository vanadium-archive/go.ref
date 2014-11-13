package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"veyron.io/veyron/veyron/lib/unixfd"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

const PrincipalHandleByteSize = sha512.Size

type keyHandle [PrincipalHandleByteSize]byte

type agentd struct {
	principal security.Principal
}

type keymgr struct {
	path       string
	principals map[keyHandle]security.Principal // GUARDED_BY(Mutex)
	passphrase []byte
	runtime    veyron2.Runtime
	mu         sync.Mutex
}

// RunAnonymousAgent starts the agent server listening on an
// anonymous unix domain socket. It will respond to requests
// using 'principal'.
// The returned 'client' is typically passed via cmd.ExtraFiles to a child process.
func RunAnonymousAgent(runtime veyron2.Runtime, principal security.Principal) (client *os.File, err error) {
	local, remote, err := unixfd.Socketpair()
	if err != nil {
		return nil, err
	}
	if err = startAgent(local, runtime, principal); err != nil {
		return nil, err
	}
	return remote, err
}

// RunKeyManager starts the key manager server listening on an
// anonymous unix domain socket. It will persist principals in 'path' using 'passphrase'.
// Typically only used by the node manager.
// The returned 'client' is typically passed via cmd.ExtraFiles to a child process.
func RunKeyManager(runtime veyron2.Runtime, path string, passphrase []byte) (client *os.File, err error) {
	if path == "" {
		return nil, verror.BadArgf("storage path is required")
	}

	mgr := &keymgr{path: path, passphrase: passphrase, principals: make(map[keyHandle]security.Principal), runtime: runtime}

	if err := os.MkdirAll(filepath.Join(mgr.path, "keys"), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(mgr.path, "creds"), 0700); err != nil {
		return nil, err
	}

	local, client, err := unixfd.Socketpair()
	if err != nil {
		return nil, err
	}

	go mgr.readNMConns(local)

	return client, nil
}

func (a keymgr) readNMConns(conn *net.UnixConn) {
	defer conn.Close()
	var buf keyHandle
	for {
		addr, n, err := unixfd.ReadConnection(conn, buf[:])
		if err == io.EOF {
			return
		} else if err != nil {
			vlog.Infof("Error accepting connection: %v", err)
			continue
		}
		var principal security.Principal
		if n == len(buf) {
			principal = a.readKey(buf)
		} else if n == 1 {
			var handle []byte
			if handle, principal, err = a.newKey(buf[0] == 1); err != nil {
				vlog.Infof("Error creating key: %v", err)
				unixfd.CloseUnixAddr(addr)
				continue
			}
			if _, err = conn.Write(handle); err != nil {
				vlog.Infof("Error sending key handle: %v", err)
				unixfd.CloseUnixAddr(addr)
				continue
			}
		} else {
			vlog.Infof("invalid key: %d bytes, expected %d or 1", n, len(buf))
			unixfd.CloseUnixAddr(addr)
			continue
		}
		conn := dial(addr)
		if principal != nil && conn != nil {
			if err := startAgent(conn, a.runtime, principal); err != nil {
				vlog.Infof("error starting agent: %v", err)
			}
		}
	}
}

func (a *keymgr) readKey(handle keyHandle) security.Principal {
	a.mu.Lock()
	cachedKey, ok := a.principals[handle]
	a.mu.Unlock()
	if ok {
		return cachedKey
	}
	filename := base64.URLEncoding.EncodeToString(handle[:])
	in, err := os.Open(filepath.Join(a.path, "keys", filename))
	if err != nil {
		vlog.Errorf("unable to open key file: %v", err)
		return nil
	}
	defer in.Close()
	key, err := vsecurity.LoadPEMKey(in, a.passphrase)
	if err != nil {
		vlog.Errorf("unable to load key: %v", err)
		return nil
	}
	state, err := vsecurity.NewPrincipalStateSerializer(filepath.Join(a.path, "creds", filename))
	if err != nil {
		vlog.Errorf("unable to create persisted state serializer: %v", err)
		return nil
	}
	principal, err := vsecurity.NewPrincipalFromSigner(security.NewInMemoryECDSASigner(key.(*ecdsa.PrivateKey)), state)
	if err != nil {
		vlog.Errorf("unable to load principal: %v", err)
		return nil
	}
	return principal
}

func dial(addr net.Addr) *net.UnixConn {
	fd, err := strconv.ParseInt(addr.String(), 10, 32)
	if err != nil {
		vlog.Errorf("Invalid address %v", addr)
		return nil
	}
	file := os.NewFile(uintptr(fd), "client")
	defer file.Close()
	conn, err := net.FileConn(file)
	if err != nil {
		vlog.Infof("unable to create conn: %v", err)
	}
	return conn.(*net.UnixConn)
}

func startAgent(conn *net.UnixConn, runtime veyron2.Runtime, principal security.Principal) error {
	agent := &agentd{principal: principal}
	serverAgent := AgentServer(agent)
	go func() {
		buf := make([]byte, 1)
		for {
			clientAddr, _, err := unixfd.ReadConnection(conn, buf)
			if err == io.EOF {
				return
			}
			if clientAddr != nil {
				// VCSecurityNone is safe since we're using anonymous unix sockets.
				// Only our child process can possibly communicate on the socket.
				//
				// Also, VCSecurityNone implies that s (ipc.Server) created below does not
				// authenticate to clients, so runtime.Principal is irrelevant for the agent.
				// TODO(ribrdb): Shutdown these servers when the connection is closed.
				s, err := runtime.NewServer(options.VCSecurityNone)
				if err != nil {
					vlog.Infof("Error creating server: %v", err)
					continue
				}
				spec := ipc.ListenSpec{Protocol: clientAddr.Network(), Address: clientAddr.String()}
				if _, err = s.Listen(spec); err == nil {
					err = s.Serve("", serverAgent, nil)
				}
			}
			if err != nil {
				vlog.Infof("Error accepting connection: %v", err)
			}
		}
	}()
	return nil
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

func (a keymgr) newKey(in_memory bool) (id []byte, p security.Principal, err error) {
	if a.path == "" {
		return nil, nil, verror.NoAccessf("not running in multi-key mode")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	keyHandle, err := keyid(key)
	if err != nil {
		return nil, nil, err
	}
	signer := security.NewInMemoryECDSASigner(key)
	if in_memory {
		p, err = vsecurity.NewPrincipalFromSigner(signer, nil)
		if err != nil {
			return nil, nil, err
		}
		a.principals[keyHandle] = p
	} else {
		filename := base64.URLEncoding.EncodeToString(keyHandle[:])
		out, err := os.OpenFile(filepath.Join(a.path, "keys", filename), os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return nil, nil, err
		}
		defer out.Close()
		err = vsecurity.SavePEMKey(out, key, a.passphrase)
		if err != nil {
			return nil, nil, err
		}
		state, err := vsecurity.NewPrincipalStateSerializer(filepath.Join(a.path, "creds", filename))
		if err != nil {
			return nil, nil, err
		}
		p, err = vsecurity.NewPrincipalFromSigner(signer, state)
		if err != nil {
			return nil, nil, err
		}
	}
	return keyHandle[:], p, nil
}

func keyid(key *ecdsa.PrivateKey) (handle keyHandle, err error) {
	slice, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return
	}
	return sha512.Sum512(slice), nil
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

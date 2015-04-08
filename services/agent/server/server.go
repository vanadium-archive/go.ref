// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/services/agent/internal/unixfd"
)

const PrincipalHandleByteSize = sha512.Size

const pkgPath = "v.io/x/ref/services/agent/server"

// Errors
var (
	errStoragePathRequired = verror.Register(pkgPath+".errStoragePathRequired",
		verror.NoRetry, "{1:}{2:} RunKeyManager: storage path is required")
	errNotMultiKeyMode = verror.Register(pkgPath+".errNotMultiKeyMode",
		verror.NoRetry, "{1:}{2:} Not running in multi-key mode")
)

type keyHandle [PrincipalHandleByteSize]byte

type agentd struct {
	id        int
	w         *watchers
	principal security.Principal
	ctx       *context.T
}

type keyData struct {
	w *watchers
	p security.Principal
}

type keymgr struct {
	path       string
	principals map[keyHandle]keyData // GUARDED_BY(Mutex)
	passphrase []byte
	ctx        *context.T
	mu         sync.Mutex
}

// RunAnonymousAgent starts the agent server listening on an
// anonymous unix domain socket. It will respond to requests
// using 'principal'.
// The returned 'client' is typically passed via cmd.ExtraFiles to a child process.
func RunAnonymousAgent(ctx *context.T, principal security.Principal) (client *os.File, err error) {
	local, remote, err := unixfd.Socketpair()
	if err != nil {
		return nil, err
	}
	if err = startAgent(ctx, local, newWatchers(), principal); err != nil {
		return nil, err
	}
	return remote, err
}

// RunKeyManager starts the key manager server listening on an
// anonymous unix domain socket. It will persist principals in 'path' using 'passphrase'.
// Typically only used by the device manager.
// The returned 'client' is typically passed via cmd.ExtraFiles to a child process.
func RunKeyManager(ctx *context.T, path string, passphrase []byte) (client *os.File, err error) {
	if path == "" {
		return nil, verror.New(errStoragePathRequired, nil)
	}

	mgr := &keymgr{path: path, passphrase: passphrase, principals: make(map[keyHandle]keyData), ctx: ctx}

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

	go mgr.readDMConns(local)

	return client, nil
}

func (a keymgr) readDMConns(conn *net.UnixConn) {
	donech := a.ctx.Done()
	if donech != nil {
		go func() {
			// Shut down our read loop if the context is cancelled
			<-donech
			conn.Close()
		}()
	}
	defer conn.Close()
	var buf keyHandle
	for {
		addr, n, ack, err := unixfd.ReadConnection(conn, buf[:])
		if err == io.EOF {
			return
		} else if err != nil {
			// We ignore read errors, unless the context is cancelled.
			select {
			case <-donech:
				return
			default:
				vlog.Infof("Error accepting connection: %v", err)
				continue
			}
		}
		ack()
		var data *keyData
		if n == len(buf) {
			data = a.readKey(buf)
		} else if n == 1 {
			var handle []byte
			if handle, data, err = a.newKey(buf[0] == 1); err != nil {
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
		if data != nil && conn != nil {
			if err := startAgent(a.ctx, conn, data.w, data.p); err != nil {
				vlog.Infof("error starting agent: %v", err)
			}
		}
	}
}

func (a *keymgr) readKey(handle keyHandle) *keyData {
	a.mu.Lock()
	cachedData, ok := a.principals[handle]
	a.mu.Unlock()
	if ok {
		return &cachedData
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
	data := keyData{newWatchers(), principal}
	a.mu.Lock()
	cachedData, ok = a.principals[handle]
	if !ok {
		a.principals[handle] = data
		cachedData = data
	}
	a.mu.Unlock()

	return &cachedData
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

func startAgent(ctx *context.T, conn *net.UnixConn, w *watchers, principal security.Principal) error {
	donech := ctx.Done()
	if donech != nil {
		go func() {
			// Interrupt the read loop if the context is cancelled.
			<-donech
			conn.Close()
		}()
	}
	go func() {
		buf := make([]byte, 1)
		for {
			clientAddr, _, ack, err := unixfd.ReadConnection(conn, buf)
			if err == io.EOF {
				conn.Close()
				return
			} else if err != nil {
				// We ignore read errors, unless the context is cancelled.
				select {
				case <-donech:
					return
				default:
					vlog.Infof("Error accepting connection: %v", err)
					continue
				}
			}
			if clientAddr != nil {
				// SecurityNone is safe since we're using anonymous unix sockets.
				// Only our child process can possibly communicate on the socket.
				//
				// Also, SecurityNone implies that s (rpc.Server) created below does not
				// authenticate to clients, so runtime.Principal is irrelevant for the agent.
				// TODO(ribrdb): Shutdown these servers when the connection is closed.
				s, err := v23.NewServer(ctx, options.SecurityNone)
				if err != nil {
					vlog.Infof("Error creating server: %v", err)
					ack()
					continue
				}
				a := []struct{ Protocol, Address string }{
					{clientAddr.Network(), clientAddr.String()},
				}
				spec := rpc.ListenSpec{Addrs: rpc.ListenAddrs(a)}
				if _, err = s.Listen(spec); err == nil {
					agent := &agentd{w.newID(), w, principal, ctx}
					serverAgent := AgentServer(agent)
					err = s.Serve("", serverAgent, nil)
				}
				ack()
			}
			if err != nil {
				vlog.Infof("Error accepting connection: %v", err)
			}
		}
	}()
	return nil
}

func (a agentd) Bless(_ rpc.ServerCall, key []byte, with security.Blessings, extension string, caveat security.Caveat, additionalCaveats []security.Caveat) (security.Blessings, error) {
	pkey, err := security.UnmarshalPublicKey(key)
	if err != nil {
		return security.Blessings{}, err
	}
	return a.principal.Bless(pkey, with, extension, caveat, additionalCaveats...)
}

func (a agentd) BlessSelf(_ rpc.ServerCall, name string, caveats []security.Caveat) (security.Blessings, error) {
	return a.principal.BlessSelf(name, caveats...)
}

func (a agentd) Sign(_ rpc.ServerCall, message []byte) (security.Signature, error) {
	return a.principal.Sign(message)
}

func (a agentd) MintDischarge(_ rpc.ServerCall, forCaveat, caveatOnDischarge security.Caveat, additionalCaveatsOnDischarge []security.Caveat) (security.Discharge, error) {
	return a.principal.MintDischarge(forCaveat, caveatOnDischarge, additionalCaveatsOnDischarge...)
}

func (a keymgr) newKey(in_memory bool) (id []byte, data *keyData, err error) {
	if a.path == "" {
		return nil, nil, verror.New(errNotMultiKeyMode, nil)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	keyHandle, err := keyid(key)
	if err != nil {
		return nil, nil, err
	}
	signer := security.NewInMemoryECDSASigner(key)
	var p security.Principal
	if in_memory {
		p, err = vsecurity.NewPrincipalFromSigner(signer, nil)
		if err != nil {
			return nil, nil, err
		}
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
	data = &keyData{newWatchers(), p}
	a.mu.Lock()
	a.principals[keyHandle] = *data
	a.mu.Unlock()
	return keyHandle[:], data, nil
}

func keyid(key *ecdsa.PrivateKey) (handle keyHandle, err error) {
	slice, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return
	}
	return sha512.Sum512(slice), nil
}

func (a agentd) PublicKey(_ rpc.ServerCall) ([]byte, error) {
	return a.principal.PublicKey().MarshalBinary()
}

func (a agentd) BlessingsByName(_ rpc.ServerCall, name security.BlessingPattern) ([]security.Blessings, error) {
	a.w.rlock()
	defer a.w.runlock()
	return a.principal.BlessingsByName(name), nil
}

func (a agentd) BlessingsInfo(_ rpc.ServerCall, blessings security.Blessings) (map[string][]security.Caveat, error) {
	a.w.rlock()
	defer a.w.runlock()
	return a.principal.BlessingsInfo(blessings), nil
}

func (a agentd) AddToRoots(_ rpc.ServerCall, blessings security.Blessings) error {
	a.w.lock()
	defer a.w.unlock(a.id)
	return a.principal.AddToRoots(blessings)
}

func (a agentd) BlessingStoreSet(_ rpc.ServerCall, blessings security.Blessings, forPeers security.BlessingPattern) (security.Blessings, error) {
	a.w.lock()
	defer a.w.unlock(a.id)
	return a.principal.BlessingStore().Set(blessings, forPeers)
}

func (a agentd) BlessingStoreForPeer(_ rpc.ServerCall, peerBlessings []string) (security.Blessings, error) {
	a.w.rlock()
	defer a.w.runlock()
	return a.principal.BlessingStore().ForPeer(peerBlessings...), nil
}

func (a agentd) BlessingStoreSetDefault(_ rpc.ServerCall, blessings security.Blessings) error {
	a.w.lock()
	defer a.w.unlock(a.id)
	return a.principal.BlessingStore().SetDefault(blessings)
}

func (a agentd) BlessingStorePeerBlessings(_ rpc.ServerCall) (map[security.BlessingPattern]security.Blessings, error) {
	a.w.rlock()
	defer a.w.runlock()
	return a.principal.BlessingStore().PeerBlessings(), nil
}

func (a agentd) BlessingStoreDebugString(_ rpc.ServerCall) (string, error) {
	a.w.rlock()
	defer a.w.runlock()
	return a.principal.BlessingStore().DebugString(), nil
}

func (a agentd) BlessingStoreDefault(_ rpc.ServerCall) (security.Blessings, error) {
	a.w.rlock()
	defer a.w.runlock()
	return a.principal.BlessingStore().Default(), nil
}

func (a agentd) BlessingRootsAdd(_ rpc.ServerCall, root []byte, pattern security.BlessingPattern) error {
	pkey, err := security.UnmarshalPublicKey(root)
	if err != nil {
		return err
	}
	a.w.lock()
	defer a.w.unlock(a.id)
	return a.principal.Roots().Add(pkey, pattern)
}

func (a agentd) BlessingRootsRecognized(_ rpc.ServerCall, root []byte, blessing string) error {
	pkey, err := security.UnmarshalPublicKey(root)
	if err != nil {
		return err
	}
	a.w.rlock()
	defer a.w.runlock()
	return a.principal.Roots().Recognized(pkey, blessing)
}

func (a agentd) BlessingRootsDebugString(_ rpc.ServerCall) (string, error) {
	a.w.rlock()
	defer a.w.runlock()
	return a.principal.Roots().DebugString(), nil
}

func (a agentd) NotifyWhenChanged(call AgentNotifyWhenChangedServerCall) error {
	ch := a.w.register(a.id)
	defer a.w.unregister(a.id, ch)
	for {
		select {
		case <-a.ctx.Done():
			return nil
		case <-call.Context().Done():
			return nil
		case _, ok := <-ch:
			if !ok {
				return nil
			}
			if err := call.SendStream().Send(true); err != nil {
				return err
			}
		}
	}
}

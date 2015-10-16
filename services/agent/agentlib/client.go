// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package agentlib implements a client for communicating with an agentd process
// holding the private key for an identity.
package agentlib

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
	"v.io/x/ref/internal/logger"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/internal/cache"
	"v.io/x/ref/services/agent/internal/ipc"
	"v.io/x/ref/services/agent/internal/unixfd"
)

const pkgPath = "v.io/x/ref/services/agent/agentlib"

// Errors
var (
	errInvalidProtocol = verror.Register(pkgPath+".errInvalidProtocol",
		verror.NoRetry, "{1:}{2:} invalid agent protocol {3}")
)

type client struct {
	caller caller
	key    security.PublicKey
}

type caller interface {
	call(name string, results []interface{}, args ...interface{}) error
	io.Closer
}

type ipcCaller struct {
	conn  *ipc.IPCConn
	flush func()
	mu    sync.Mutex
}

func (i *ipcCaller) call(name string, results []interface{}, args ...interface{}) error {
	return i.conn.Call(name, args, results...)
}

func (i *ipcCaller) Close() error {
	i.conn.Close()
	return nil
}

func (i *ipcCaller) FlushAllCaches() error {
	var flush func()
	i.mu.Lock()
	flush = i.flush
	i.mu.Unlock()
	if flush != nil {
		flush()
	}
	return nil
}

type vrpcCaller struct {
	ctx    *context.T
	client rpc.Client
	name   string
	cancel func()
}

func (c *vrpcCaller) Close() error {
	c.cancel()
	return nil
}

func (c *vrpcCaller) call(name string, results []interface{}, args ...interface{}) error {
	call, err := c.startCall(name, args...)
	if err != nil {
		return err
	}
	if err := call.Finish(results...); err != nil {
		return err
	}
	return nil
}

func (c *vrpcCaller) startCall(name string, args ...interface{}) (rpc.ClientCall, error) {
	ctx, _ := vtrace.WithNewTrace(c.ctx)
	// SecurityNone is safe here since we're using anonymous unix sockets.
	return c.client.StartCall(ctx, c.name, name, args, options.SecurityNone, options.Preresolved{})
}

func results(inputs ...interface{}) []interface{} {
	return inputs
}

func newUncachedPrincipalX(path string) (*client, error) {
	caller := new(ipcCaller)
	i := ipc.NewIPC()
	i.Serve(caller)
	conn, err := i.Connect(path)
	if err != nil {
		return nil, err
	}
	caller.conn = conn
	agent := &client{caller: caller}
	if err := agent.fetchPublicKey(); err != nil {
		return nil, err
	}
	return agent, nil
}

// NewAgentPrincipal returns a security.Pricipal using the PrivateKey held in a remote agent process.
// 'path' is the path to the agent socket, typically obtained from
// os.GetEnv(envvar.AgentAddress).
func NewAgentPrincipalX(path string) (agent.Principal, error) {
	p, err := newUncachedPrincipalX(path)
	if err != nil {
		return nil, err
	}
	cached, flush, err := cache.NewCachedPrincipalX(p)
	if err != nil {
		return nil, err
	}
	caller := p.caller.(*ipcCaller)
	caller.mu.Lock()
	caller.flush = flush
	caller.mu.Unlock()
	return cached, nil
}

// NewAgentPrincipal returns a security.Pricipal using the PrivateKey held in a remote agent process.
// 'endpoint' is the endpoint for connecting to the agent, typically obtained from
// os.GetEnv(envvar.AgentEndpoint).
// 'ctx' should not have a deadline, and should never be cancelled while the
// principal is in use.
func NewAgentPrincipal(ctx *context.T, endpoint naming.Endpoint, insecureClient rpc.Client) (security.Principal, error) {
	p, err := newUncachedPrincipal(ctx, endpoint, insecureClient)
	if err != nil {
		return p, err
	}
	caller := p.caller.(*vrpcCaller)
	call, callErr := caller.startCall("NotifyWhenChanged")
	if callErr != nil {
		return nil, callErr
	}
	return cache.NewCachedPrincipal(caller.ctx, p, call)
}
func newUncachedPrincipal(ctx *context.T, ep naming.Endpoint, insecureClient rpc.Client) (*client, error) {
	// This isn't a real vanadium endpoint. It contains the vanadium version
	// info, but the address is serving the agent protocol.
	if ep.Addr().Network() != "" {
		return nil, verror.New(errInvalidProtocol, ctx, ep.Addr().Network())
	}
	fd, err := strconv.Atoi(ep.Addr().String())
	if err != nil {
		return nil, err
	}
	syscall.ForkLock.Lock()
	fd, err = syscall.Dup(fd)
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.Unlock()
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "agent_client")
	defer f.Close()
	conn, err := net.FileConn(f)
	if err != nil {
		return nil, err
	}
	// This is just an arbitrary 1 byte string. The value is ignored.
	data := make([]byte, 1)
	addr, err := unixfd.SendConnection(conn.(*net.UnixConn), data)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	caller := &vrpcCaller{
		client: insecureClient,
		name:   naming.JoinAddressName(agentEndpoint("unixfd", addr.String()), ""),
		ctx:    ctx,
		cancel: cancel,
	}
	agent := &client{caller: caller}
	if err := agent.fetchPublicKey(); err != nil {
		return nil, err
	}
	return agent, nil
}

func (c *client) Close() error {
	return c.caller.Close()
}

func (c *client) fetchPublicKey() (err error) {
	var b []byte
	if err = c.caller.call("PublicKey", results(&b)); err != nil {
		return
	}
	c.key, err = security.UnmarshalPublicKey(b)
	return
}

func (c *client) Bless(key security.PublicKey, with security.Blessings, extension string, caveat security.Caveat, additionalCaveats ...security.Caveat) (security.Blessings, error) {
	var blessings security.Blessings
	marshalledKey, err := key.MarshalBinary()
	if err != nil {
		return security.Blessings{}, err
	}
	err = c.caller.call("Bless", results(&blessings), marshalledKey, with, extension, caveat, additionalCaveats)
	return blessings, err
}

func (c *client) BlessSelf(name string, caveats ...security.Caveat) (security.Blessings, error) {
	var blessings security.Blessings
	err := c.caller.call("BlessSelf", results(&blessings), name, caveats)
	return blessings, err
}

func (c *client) Sign(message []byte) (sig security.Signature, err error) {
	err = c.caller.call("Sign", results(&sig), message)
	return
}

func (c *client) MintDischarge(forCaveat, caveatOnDischarge security.Caveat, additionalCaveatsOnDischarge ...security.Caveat) (security.Discharge, error) {
	var discharge security.Discharge
	if err := c.caller.call("MintDischarge", results(&discharge), forCaveat, caveatOnDischarge, additionalCaveatsOnDischarge); err != nil {
		return security.Discharge{}, err
	}
	return discharge, nil
}

func (c *client) PublicKey() security.PublicKey {
	return c.key
}

func (c *client) BlessingStore() security.BlessingStore {
	return &blessingStore{caller: c.caller, key: c.key}
}

func (c *client) Roots() security.BlessingRoots {
	return &blessingRoots{c.caller}
}

// TODO(ataly): Implement this method.
func (c *client) Encrypter() security.BlessingsBasedEncrypter {
	return nil
}

// TODO(ataly): Implement this method.
func (c *client) Decrypter() security.BlessingsBasedDecrypter {
	return nil
}

type blessingStore struct {
	caller caller
	key    security.PublicKey
}

func (b *blessingStore) Set(blessings security.Blessings, forPeers security.BlessingPattern) (security.Blessings, error) {
	var previous security.Blessings
	err := b.caller.call("BlessingStoreSet", results(&previous), blessings, forPeers)
	return previous, err
}

func (b *blessingStore) ForPeer(peerBlessings ...string) security.Blessings {
	var blessings security.Blessings
	if err := b.caller.call("BlessingStoreForPeer", results(&blessings), peerBlessings); err != nil {
		logger.Global().Infof("error calling BlessingStorePeerBlessings: %v", err)
	}
	return blessings
}

func (b *blessingStore) SetDefault(blessings security.Blessings) error {
	return b.caller.call("BlessingStoreSetDefault", results(), blessings)
}

func (b *blessingStore) Default() security.Blessings {
	var blessings security.Blessings
	err := b.caller.call("BlessingStoreDefault", results(&blessings))
	if err != nil {
		logger.Global().Infof("error calling BlessingStoreDefault: %v", err)
		return security.Blessings{}
	}
	return blessings
}

func (b *blessingStore) PublicKey() security.PublicKey {
	return b.key
}

func (b *blessingStore) PeerBlessings() map[security.BlessingPattern]security.Blessings {
	var bmap map[security.BlessingPattern]security.Blessings
	err := b.caller.call("BlessingStorePeerBlessings", results(&bmap))
	if err != nil {
		logger.Global().Infof("error calling BlessingStorePeerBlessings: %v", err)
		return nil
	}
	return bmap
}

func (b *blessingStore) DebugString() (s string) {
	err := b.caller.call("BlessingStoreDebugString", results(&s))
	if err != nil {
		s = fmt.Sprintf("error calling BlessingStoreDebugString: %v", err)
		logger.Global().Infof(s)
	}
	return
}

func (b *blessingStore) CacheDischarge(d security.Discharge, c security.Caveat, i security.DischargeImpetus) {
	err := b.caller.call("BlessingStoreCacheDischarge", results(), d, c, i)
	if err != nil {
		logger.Global().Infof("error calling BlessingStoreCacheDischarge: %v", err)
	}
}

func (b *blessingStore) ClearDischarges(discharges ...security.Discharge) {
	err := b.caller.call("BlessingStoreClearDischarges", results(), discharges)
	if err != nil {
		logger.Global().Infof("error calling BlessingStoreClearDischarges: %v", err)
	}
}

func (b *blessingStore) Discharge(caveat security.Caveat, impetus security.DischargeImpetus) (out security.Discharge) {
	err := b.caller.call("BlessingStoreDischarge", results(&out), caveat, impetus)
	if err != nil {
		logger.Global().Infof("error calling BlessingStoreDischarge: %v", err)
	}
	return
}

type blessingRoots struct {
	caller caller
}

func (b *blessingRoots) Add(root []byte, pattern security.BlessingPattern) error {
	return b.caller.call("BlessingRootsAdd", results(), root, pattern)
}

func (b *blessingRoots) Recognized(root []byte, blessing string) error {
	return b.caller.call("BlessingRootsRecognized", results(), root, blessing)
}

func (b *blessingRoots) Dump() map[security.BlessingPattern][]security.PublicKey {
	var marshaledRoots map[security.BlessingPattern][][]byte
	if err := b.caller.call("BlessingRootsDump", results(&marshaledRoots)); err != nil {
		logger.Global().Infof("error calling BlessingRootsDump: %v", err)
		return nil
	}
	ret := make(map[security.BlessingPattern][]security.PublicKey)
	for p, marshaledKeys := range marshaledRoots {
		for _, marshaledKey := range marshaledKeys {
			key, err := security.UnmarshalPublicKey(marshaledKey)
			if err != nil {
				logger.Global().Infof("security.UnmarshalPublicKey(%v) returned error: %v", marshaledKey, err)
				continue
			}
			ret[p] = append(ret[p], key)
		}
	}
	return ret
}

func (b *blessingRoots) DebugString() (s string) {
	err := b.caller.call("BlessingRootsDebugString", results(&s))
	if err != nil {
		s = fmt.Sprintf("error calling BlessingRootsDebugString: %v", err)
		logger.Global().Infof(s)
	}
	return
}

func agentEndpoint(proto, addr string) string {
	// TODO: use naming.FormatEndpoint when it supports version 6.
	return fmt.Sprintf("@6@%s@%s@@@s@@@", proto, addr)
}

func AgentEndpoint(fd int) string {
	// We use an empty protocol here because this isn't really speaking
	// veyron rpc.
	return agentEndpoint("", fmt.Sprintf("%d", fd))
}

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
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"v.io/v23/security"
	"v.io/v23/verror"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/internal/ipc"
	"v.io/x/ref/services/agent/internal/lockfile"
)

const pkgPath = "v.io/x/ref/services/agent/internal/server"

// Errors
var (
	errStoragePathRequired = verror.Register(pkgPath+".errStoragePathRequired",
		verror.NoRetry, "{1:}{2:} RunKeyManager: storage path is required")
	errNotMultiKeyMode = verror.Register(pkgPath+".errNotMultiKeyMode",
		verror.NoRetry, "{1:}{2:} Not running in multi-key mode")
)

type agentd struct {
	ipc       *ipc.IPC
	principal security.Principal
	mu        sync.RWMutex
}

type keyData struct {
	p     security.Principal
	agent *ipc.IPC
	path  string
}

type keymgr struct {
	path       string
	passphrase []byte
	cache      map[[agent.PrincipalHandleByteSize]byte]keyData
	mu         sync.Mutex
}

// ServeAgent registers the agent server with 'ipc'.
// It will respond to requests using 'principal'.
// Must be called before ipc.Listen or ipc.Connect.
func ServeAgent(i *ipc.IPC, principal security.Principal) (err error) {
	server := &agentd{ipc: i, principal: principal}
	return i.Serve(server)
}

func newKeyManager(path string, passphrase []byte) (*keymgr, error) {
	if path == "" {
		return nil, verror.New(errStoragePathRequired, nil)
	}

	mgr := &keymgr{path: path, passphrase: passphrase, cache: make(map[[agent.PrincipalHandleByteSize]byte]keyData)}

	if err := os.MkdirAll(filepath.Join(mgr.path, "keys"), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(mgr.path, "creds"), 0700); err != nil {
		return nil, err
	}
	return mgr, nil
}

type localKeymgr struct {
	*keymgr
}

func NewLocalKeyManager(path string, passphrase []byte) (agent.KeyManager, error) {
	m, err := newKeyManager(path, passphrase)
	return localKeymgr{m}, err
}

func (l localKeymgr) Close() error {
	defer l.mu.Unlock()
	l.mu.Lock()
	for _, data := range l.cache {
		if data.agent != nil {
			data.agent.Close()
		}
	}
	return nil
}

// ServeKeyManager registers key manager server with 'ipc'.
// It will persist principals in 'path' using 'passphrase'.
// Must be called before ipc.Listen or ipc.Connect.
func ServeKeyManager(i *ipc.IPC, path string, passphrase []byte) error {
	mgr, err := newKeyManager(path, passphrase)
	if err != nil {
		return err
	}
	return i.Serve(mgr)
}

func (a *keymgr) readKey(handle [agent.PrincipalHandleByteSize]byte) (keyData, error) {
	{
		a.mu.Lock()
		cached, ok := a.cache[handle]
		a.mu.Unlock()
		if ok {
			return cached, nil
		}
	}

	var nodata keyData
	filename := base64.URLEncoding.EncodeToString(handle[:])
	in, err := os.Open(filepath.Join(a.path, "keys", filename))
	if err != nil {
		return nodata, fmt.Errorf("unable to open key file: %v", err)
	}
	defer in.Close()
	key, err := vsecurity.LoadPEMKey(in, a.passphrase)
	if err != nil {
		return nodata, fmt.Errorf("unable to load key in %q: %v", in.Name(), err)
	}
	state, err := vsecurity.NewPrincipalStateSerializer(filepath.Join(a.path, "creds", filename))
	if err != nil {
		return nodata, fmt.Errorf("unable to create persisted state serializer: %v", err)
	}
	principal, err := vsecurity.NewPrincipalFromSigner(security.NewInMemoryECDSASigner(key.(*ecdsa.PrivateKey)), state)
	if err != nil {
		return nodata, fmt.Errorf("unable to load principal: %v", err)
	}
	data := keyData{p: principal}
	a.mu.Lock()
	if cachedData, ok := a.cache[handle]; ok {
		data = cachedData
	} else {
		a.cache[handle] = data
	}
	a.mu.Unlock()
	return data, nil
}

func (a *agentd) Bless(key []byte, with security.Blessings, extension string, caveat security.Caveat, additionalCaveats []security.Caveat) (security.Blessings, error) {
	pkey, err := security.UnmarshalPublicKey(key)
	if err != nil {
		return security.Blessings{}, err
	}
	return a.principal.Bless(pkey, with, extension, caveat, additionalCaveats...)
}

func (a *agentd) BlessSelf(name string, caveats []security.Caveat) (security.Blessings, error) {
	return a.principal.BlessSelf(name, caveats...)
}

func (a *agentd) Sign(message []byte) (security.Signature, error) {
	return a.principal.Sign(message)
}

func (a *agentd) MintDischarge(forCaveat, caveatOnDischarge security.Caveat, additionalCaveatsOnDischarge []security.Caveat) (security.Discharge, error) {
	return a.principal.MintDischarge(forCaveat, caveatOnDischarge, additionalCaveatsOnDischarge...)
}

func (a *keymgr) NewPrincipal(in_memory bool) (handle [agent.PrincipalHandleByteSize]byte, err error) {
	if a.path == "" {
		return handle, verror.New(errNotMultiKeyMode, nil)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return handle, err
	}
	if handle, err = keyid(key); err != nil {
		return handle, err
	}
	signer := security.NewInMemoryECDSASigner(key)
	var p security.Principal
	if in_memory {
		if p, err = vsecurity.NewPrincipalFromSigner(signer, nil); err != nil {
			return handle, err
		}
	} else {
		filename := base64.URLEncoding.EncodeToString(handle[:])
		out, err := os.OpenFile(filepath.Join(a.path, "keys", filename), os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return handle, err
		}
		defer out.Close()
		err = vsecurity.SavePEMKey(out, key, a.passphrase)
		if err != nil {
			return handle, err
		}
		state, err := vsecurity.NewPrincipalStateSerializer(filepath.Join(a.path, "creds", filename))
		if err != nil {
			return handle, err
		}
		p, err = vsecurity.NewPrincipalFromSigner(signer, state)
		if err != nil {
			return handle, err
		}
	}
	data := keyData{p: p}
	a.mu.Lock()
	if _, ok := a.cache[handle]; !ok {
		a.cache[handle] = data
	}
	a.mu.Unlock()
	return handle, nil
}

func keyid(key *ecdsa.PrivateKey) (handle [agent.PrincipalHandleByteSize]byte, err error) {
	slice, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return
	}
	return sha512.Sum512(slice), nil
}

func (a *agentd) unlock() {
	a.mu.Unlock()
	for _, conn := range a.ipc.Connections() {
		go conn.Call("FlushAllCaches", nil)
	}
}

func (a *agentd) PublicKey() ([]byte, error) {
	return a.principal.PublicKey().MarshalBinary()
}

func (a *agentd) BlessingStoreSet(blessings security.Blessings, forPeers security.BlessingPattern) (security.Blessings, error) {
	defer a.unlock()
	a.mu.Lock()
	return a.principal.BlessingStore().Set(blessings, forPeers)
}

func (a *agentd) BlessingStoreForPeer(peerBlessings []string) (security.Blessings, error) {
	defer a.mu.RUnlock()
	a.mu.RLock()
	return a.principal.BlessingStore().ForPeer(peerBlessings...), nil
}

func (a *agentd) BlessingStoreSetDefault(blessings security.Blessings) error {
	defer a.unlock()
	a.mu.Lock()
	return a.principal.BlessingStore().SetDefault(blessings)
}

func (a *agentd) BlessingStorePeerBlessings() (map[security.BlessingPattern]security.Blessings, error) {
	defer a.mu.RUnlock()
	a.mu.RLock()
	return a.principal.BlessingStore().PeerBlessings(), nil
}

func (a *agentd) BlessingStoreDebugString() (string, error) {
	defer a.mu.RUnlock()
	a.mu.RLock()
	return a.principal.BlessingStore().DebugString(), nil
}

func (a *agentd) BlessingStoreDefault() (security.Blessings, error) {
	defer a.mu.RUnlock()
	a.mu.RLock()
	b, _ := a.principal.BlessingStore().Default()
	return b, nil
}

func (a *agentd) BlessingStoreCacheDischarge(discharge security.Discharge, caveat security.Caveat, impetus security.DischargeImpetus) error {
	defer a.mu.Unlock()
	a.mu.Lock()
	a.principal.BlessingStore().CacheDischarge(discharge, caveat, impetus)
	return nil
}

func (a *agentd) BlessingStoreClearDischarges(discharges []security.Discharge) error {
	defer a.mu.Unlock()
	a.mu.Lock()
	a.principal.BlessingStore().ClearDischarges(discharges...)
	return nil
}

func (a *agentd) BlessingStoreDischarge(caveat security.Caveat, impetus security.DischargeImpetus) (security.Discharge, error) {
	defer a.mu.Unlock()
	a.mu.Lock()
	discharge, _ := a.principal.BlessingStore().Discharge(caveat, impetus)
	return discharge, nil
}

func (a *agentd) BlessingStoreDischarge2(caveat security.Caveat, impetus security.DischargeImpetus) (security.Discharge, time.Time, error) {
	defer a.mu.Unlock()
	a.mu.Lock()
	discharge, cacheTime := a.principal.BlessingStore().Discharge(caveat, impetus)
	return discharge, cacheTime, nil
}

func (a *agentd) BlessingRootsAdd(root []byte, pattern security.BlessingPattern) error {
	defer a.unlock()
	a.mu.Lock()
	return a.principal.Roots().Add(root, pattern)
}

func (a *agentd) BlessingRootsRecognized(root []byte, blessing string) error {
	defer a.mu.RUnlock()
	a.mu.RLock()
	return a.principal.Roots().Recognized(root, blessing)
}

func (a *agentd) BlessingRootsDump() (map[security.BlessingPattern][][]byte, error) {
	ret := make(map[security.BlessingPattern][][]byte)
	defer a.mu.RUnlock()
	a.mu.RLock()
	for p, keys := range a.principal.Roots().Dump() {
		for _, key := range keys {
			marshaledKey, err := key.MarshalBinary()
			if err != nil {
				return nil, err
			}
			ret[p] = append(ret[p], marshaledKey)
		}
	}
	return ret, nil
}

func (a *agentd) BlessingRootsDebugString() (string, error) {
	defer a.mu.RUnlock()
	a.mu.RLock()
	return a.principal.Roots().DebugString(), nil
}

func (m *keymgr) ServePrincipal(handle [agent.PrincipalHandleByteSize]byte, path string) error {
	if maxLen := GetMaxSockPathLen(); len(path) > maxLen {
		return fmt.Errorf("socket path (%s) exceeds maximum allowed socket path length (%d)", path, maxLen)
	}
	if _, err := m.readKey(handle); err != nil {
		return err
	}
	defer m.mu.Unlock()
	m.mu.Lock()
	data, ok := m.cache[handle]
	if !ok {
		return fmt.Errorf("key deleted")
	}
	if data.agent != nil {
		return verror.NewErrExist(nil)
	}
	ipc := ipc.NewIPC()
	if err := ServeAgent(ipc, data.p); err != nil {
		return err
	}
	if err := lockfile.CreateLockfile(path); err != nil {
		return err
	}
	if err := ipc.Listen(path); err != nil {
		return err
	}
	data.agent = ipc
	data.path = path
	m.cache[handle] = data
	return nil
}

func (m *keymgr) StopServing(handle [agent.PrincipalHandleByteSize]byte) error {
	if _, err := m.readKey(handle); err != nil {
		return err
	}
	defer m.mu.Unlock()
	m.mu.Lock()
	data, ok := m.cache[handle]
	if !ok {
		return fmt.Errorf("key deleted")
	}
	if data.agent == nil {
		return verror.NewErrNoExist(nil)
	}
	if len(data.path) > 0 {
		lockfile.RemoveLockfile(data.path)
	}
	data.agent.Close()
	data.agent = nil
	data.path = ""
	m.cache[handle] = data
	return nil
}

func (m *keymgr) DeletePrincipal(handle [agent.PrincipalHandleByteSize]byte) error {
	defer m.mu.Unlock()
	m.mu.Lock()
	data, cached := m.cache[handle]
	if cached {
		if data.agent != nil {
			data.agent.Close()
		}
		delete(m.cache, handle)
	}
	filename := base64.URLEncoding.EncodeToString(handle[:])
	keyErr := os.Remove(filepath.Join(m.path, "keys", filename))
	credErr := os.RemoveAll(filepath.Join(m.path, "creds", filename))
	if os.IsNotExist(keyErr) && !cached {
		return verror.NewErrNoExist(nil)
	} else if keyErr != nil {
		return keyErr
	}
	return credErr
}

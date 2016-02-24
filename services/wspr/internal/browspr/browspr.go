// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Browspr is the browser version of WSPR, intended to communicate with javascript through postMessage.
package browspr

import (
	"fmt"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	"v.io/x/ref/lib/security"
	"v.io/x/ref/services/wspr/internal/account"
	"v.io/x/ref/services/wspr/internal/app"
	"v.io/x/ref/services/wspr/internal/principal"
)

// Browspr is an intermediary between our javascript code and the vanadium
// network that allows our javascript library to use vanadium.
type Browspr struct {
	ctx              *context.T
	listenSpec       *rpc.ListenSpec
	namespaceRoots   []string
	accountManager   *account.AccountManager
	postMessage      func(instanceId int32, ty, msg string)
	principalManager *principal.PrincipalManager

	mu              sync.Mutex
	activeInstances map[int32]*pipe // GUARDED_BY mu
}

// Create a new Browspr instance.
func NewBrowspr(ctx *context.T,
	postMessage func(instanceId int32, ty, msg string),
	listenSpec *rpc.ListenSpec,
	identd string,
	wsNamespaceRoots []string,
	principalSerializer security.SerializerReaderWriter) *Browspr {
	if listenSpec.Proxy == "" {
		ctx.Fatalf("a vanadium proxy must be set")
	}
	if identd == "" {
		ctx.Fatalf("an identd server must be set")
	}

	browspr := &Browspr{
		listenSpec:      listenSpec,
		namespaceRoots:  wsNamespaceRoots,
		postMessage:     postMessage,
		ctx:             ctx,
		activeInstances: make(map[int32]*pipe),
	}

	// TODO(nlacasse, bjornick) use a serializer that can actually persist.
	var err error
	p := v23.GetPrincipal(ctx)

	if principalSerializer == nil {
		ctx.Fatalf("principalSerializer must not be nil")
	}

	if browspr.principalManager, err = principal.NewPrincipalManager(p, principalSerializer); err != nil {
		ctx.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	browspr.accountManager = account.NewAccountManager(identd, browspr.principalManager)

	return browspr
}

func (b *Browspr) Shutdown() {
	// TODO(ataly, bprosnitz): Get rid of this method if possible.
}

// CreateInstance creates a new pipe and stores it in activeInstances map.
func (b *Browspr) createInstance(instanceId int32, origin string, namespaceRoots []string, proxy string) (*pipe, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Make sure we don't already have an instance.
	p, ok := b.activeInstances[instanceId]
	if ok {
		return nil, fmt.Errorf("InstanceId %v already has an open instance.", instanceId)
	}

	p = newPipe(b, instanceId, origin, namespaceRoots, proxy)
	if p == nil {
		return nil, fmt.Errorf("Could not create pipe for instanceId %v and origin %v", instanceId, origin)
	}
	b.activeInstances[instanceId] = p
	return p, nil
}

// HandleMessage handles most messages from javascript and forwards them to a
// Controller.
func (b *Browspr) HandleMessage(instanceId int32, origin string, msg app.Message) error {
	b.mu.Lock()
	p, ok := b.activeInstances[instanceId]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("No pipe found for instanceId %v. Must send CreateInstance message first.", instanceId)
	}

	if origin != p.origin {
		return fmt.Errorf("Invalid message origin. InstanceId %v has origin %v, but message is from %v.", instanceId, p.origin, origin)
	}

	return p.handleMessage(msg)
}

// HandleCleanupRpc cleans up the specified instance state. (For instance,
// when a browser tab is closed)
func (b *Browspr) HandleCleanupRpc(val *vom.RawBytes) (*vom.RawBytes, error) {
	var msg CleanupMessage
	if err := val.ToValue(&msg); err != nil {
		return nil, fmt.Errorf("HandleCleanupRpc did not receive CleanupMessage, received: %v, %v", val, err)
	}

	b.mu.Lock()

	if pipe, ok := b.activeInstances[msg.InstanceId]; ok {
		// We must unlock the mutex before calling cleanunp, otherwise
		// browspr deadlocks.
		b.mu.Unlock()
		pipe.cleanup(b.ctx)

		b.mu.Lock()
		delete(b.activeInstances, msg.InstanceId)
	}

	b.mu.Unlock()

	return nil, nil
}

// Handler for creating an account in the principal manager.
// A valid OAuth2 access token must be supplied in the request body,
// which is exchanged for blessings from the vanadium blessing server.
// An account based on the blessings is then added to WSPR's principal
// manager, and the set of blessing strings are returned to the client.
func (b *Browspr) HandleAuthCreateAccountRpc(val *vom.RawBytes) (*vom.RawBytes, error) {
	var msg CreateAccountMessage
	if err := val.ToValue(&msg); err != nil {
		return nil, fmt.Errorf("HandleAuthCreateAccountRpc did not receive CreateAccountMessage, received: %v, %v", val, err)
	}

	ctx, _ := vtrace.WithNewTrace(b.ctx)
	account, err := b.accountManager.CreateAccount(ctx, msg.Token)
	if err != nil {
		return nil, err
	}

	return vom.RawBytesFromValue(account)
}

// HandleAssociateAccountMessage associates an account with the specified origin.
func (b *Browspr) HandleAuthAssociateAccountRpc(val *vom.RawBytes) (*vom.RawBytes, error) {
	var msg AssociateAccountMessage
	if err := val.ToValue(&msg); err != nil {
		return nil, fmt.Errorf("HandleAuthAssociateAccountRpc did not receive AssociateAccountMessage, received: %v, %v", val, err)
	}
	ctx, _ := vtrace.WithNewTrace(b.ctx)
	if err := b.accountManager.AssociateAccount(ctx, msg.Origin, msg.Account, msg.Caveats); err != nil {
		return nil, err
	}
	return nil, nil
}

// HandleAuthGetAccountsRpc gets the root account name from the account manager.
func (b *Browspr) HandleAuthGetAccountsRpc(*vom.RawBytes) (*vom.RawBytes, error) {
	accounts := b.accountManager.GetAccounts()
	return vom.RawBytesFromValue(accounts)
}

// HandleAuthOriginHasAccountRpc returns true iff the origin has an associated
// principal.
func (b *Browspr) HandleAuthOriginHasAccountRpc(val *vom.RawBytes) (*vom.RawBytes, error) {
	var msg OriginHasAccountMessage
	if err := val.ToValue(&msg); err != nil {
		return nil, fmt.Errorf("HandleAuthOriginHasAccountRpc did not receive OriginHasAccountMessage, received: %v, %v", val, err)
	}

	res := b.accountManager.OriginHasAccount(msg.Origin)
	return vom.RawBytesFromValue(res)
}

// HandleCreateInstanceRpc sets the namespace root and proxy on the pipe, if
// any are provided.
func (b *Browspr) HandleCreateInstanceRpc(val *vom.RawBytes) (*vom.RawBytes, error) {
	var msg CreateInstanceMessage
	if err := val.ToValue(&msg); err != nil {
		return nil, fmt.Errorf("HandleCreateInstanceMessage did not receive CreateInstanceMessage, received: %v, %v", val, err)
	}

	_, err := b.createInstance(msg.InstanceId, msg.Origin, msg.NamespaceRoots, msg.Proxy)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

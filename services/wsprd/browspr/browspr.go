// Browspr is the browser version of WSPR, intended to communicate with javascript through postMessage.
package browspr

import (
	"fmt"
	"reflect"
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/vdl"
	"v.io/v23/vtrace"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/wsprd/account"
	"v.io/x/ref/services/wsprd/principal"
)

// Browspr is an intermediary between our javascript code and the veyron
// network that allows our javascript library to use veyron.
type Browspr struct {
	ctx              *context.T
	listenSpec       *ipc.ListenSpec
	namespaceRoots   []string
	accountManager   *account.AccountManager
	postMessage      func(instanceId int32, ty, msg string)
	principalManager *principal.PrincipalManager

	// Locks activeInstances
	mu              sync.Mutex
	activeInstances map[int32]*pipe
}

// Create a new Browspr instance.
func NewBrowspr(ctx *context.T,
	postMessage func(instanceId int32, ty, msg string),
	listenSpec *ipc.ListenSpec,
	identd string,
	wsNamespaceRoots []string) *Browspr {
	if listenSpec.Proxy == "" {
		vlog.Fatalf("a veyron proxy must be set")
	}
	if identd == "" {
		vlog.Fatalf("an identd server must be set")
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
	if browspr.principalManager, err = principal.NewPrincipalManager(p, &principal.InMemorySerializer{}); err != nil {
		vlog.Fatalf("principal.NewPrincipalManager failed: %s", err)
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
func (b *Browspr) HandleMessage(instanceId int32, origin, msg string) error {
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
func (b *Browspr) HandleCleanupRpc(val *vdl.Value) (*vdl.Value, error) {
	var msg CleanupMessage
	if err := vdl.Convert(&msg, val); err != nil {
		return nil, fmt.Errorf("HandleCleanupRpc did not receive CleanupMessage, received: %v, %v", val, err)
	}

	b.mu.Lock()

	if pipe, ok := b.activeInstances[msg.InstanceId]; ok {
		// We must unlock the mutex before calling cleanunp, otherwise
		// browspr deadlocks.
		b.mu.Unlock()
		pipe.cleanup()

		b.mu.Lock()
		delete(b.activeInstances, msg.InstanceId)
	}

	b.mu.Unlock()

	return nil, nil
}

// Handler for creating an account in the principal manager.
// A valid OAuth2 access token must be supplied in the request body,
// which is exchanged for blessings from the veyron blessing server.
// An account based on the blessings is then added to WSPR's principal
// manager, and the set of blessing strings are returned to the client.
func (b *Browspr) HandleAuthCreateAccountRpc(val *vdl.Value) (*vdl.Value, error) {
	var msg CreateAccountMessage
	if err := vdl.Convert(&msg, val); err != nil {
		return nil, fmt.Errorf("HandleAuthCreateAccountRpc did not receive CreateAccountMessage, received: %v, %v", val, err)
	}

	ctx, _ := vtrace.SetNewTrace(b.ctx)
	account, err := b.accountManager.CreateAccount(ctx, msg.Token)
	if err != nil {
		return nil, err
	}

	return vdl.ValueFromReflect(reflect.ValueOf(account))
}

// HandleAssociateAccountMessage associates an account with the specified origin.
func (b *Browspr) HandleAuthAssociateAccountRpc(val *vdl.Value) (*vdl.Value, error) {
	var msg AssociateAccountMessage
	if err := vdl.Convert(&msg, val); err != nil {
		return nil, fmt.Errorf("HandleAuthAssociateAccountRpc did not receive AssociateAccountMessage, received: %v, %v", val, err)
	}

	if err := b.accountManager.AssociateAccount(msg.Origin, msg.Account, msg.Caveats); err != nil {
		return nil, err
	}
	return nil, nil
}

// HandleAuthGetAccountsRpc gets the root account name from the account manager.
func (b *Browspr) HandleAuthGetAccountsRpc(*vdl.Value) (*vdl.Value, error) {
	return vdl.ValueFromReflect(reflect.ValueOf(b.accountManager.GetAccounts()))
}

// HandleAuthOriginHasAccountRpc returns true iff the origin has an associated
// principal.
func (b *Browspr) HandleAuthOriginHasAccountRpc(val *vdl.Value) (*vdl.Value, error) {
	var msg OriginHasAccountMessage
	if err := vdl.Convert(&msg, val); err != nil {
		return nil, fmt.Errorf("HandleAuthOriginHasAccountRpc did not receive OriginHasAccountMessage, received: %v, %v", val, err)
	}

	res := b.accountManager.OriginHasAccount(msg.Origin)
	return vdl.ValueFromReflect(reflect.ValueOf(res))
}

// HandleCreateInstanceRpc sets the namespace root and proxy on the pipe, if
// any are provided.
func (b *Browspr) HandleCreateInstanceRpc(val *vdl.Value) (*vdl.Value, error) {
	var msg CreateInstanceMessage
	if err := vdl.Convert(&msg, val); err != nil {
		return nil, fmt.Errorf("HandleCreateInstanceMessage did not receive CreateInstanceMessage, received: %v, %v", val, err)
	}

	_, err := b.createInstance(msg.InstanceId, msg.Origin, msg.NamespaceRoots, msg.Proxy)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

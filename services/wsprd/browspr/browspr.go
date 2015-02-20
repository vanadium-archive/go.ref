// Browspr is the browser version of WSPR, intended to communicate with javascript through postMessage.
package browspr

import (
	"fmt"
	"reflect"
	"sync"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vlog"
	"v.io/core/veyron2/vtrace"
	"v.io/wspr/veyron/services/wsprd/account"
	"v.io/wspr/veyron/services/wsprd/principal"
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
	p := veyron2.GetPrincipal(ctx)
	if browspr.principalManager, err = principal.NewPrincipalManager(p, &principal.InMemorySerializer{}); err != nil {
		vlog.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	browspr.accountManager = account.NewAccountManager(identd, browspr.principalManager)

	return browspr
}

func (browspr *Browspr) Shutdown() {
	// TODO(ataly, bprosnitz): Get rid of this method if possible.
}

// HandleMessage handles most messages from javascript and forwards them to a
// Controller.
func (b *Browspr) HandleMessage(instanceId int32, origin, msg string) error {
	b.mu.Lock()
	instance, ok := b.activeInstances[instanceId]
	if !ok {
		instance = newPipe(b, instanceId, origin)
		if instance == nil {
			return fmt.Errorf("Could not create pipe for origin %v: origin")
		}
		b.activeInstances[instanceId] = instance
	}
	b.mu.Unlock()

	return instance.handleMessage(msg)
}

// HandleCleanupRpc cleans up the specified instance state. (For instance,
// when a browser tab is closed)
func (b *Browspr) HandleCleanupRpc(val *vdl.Value) (*vdl.Value, error) {
	var msg CleanupMessage
	if err := vdl.Convert(&msg, val); err != nil {
		return nil, fmt.Errorf("HandleCleanupRpc did not receive CleanupMessage, received: %v, %v", val, err)
	}

	b.mu.Lock()

	if instance, ok := b.activeInstances[msg.InstanceId]; ok {
		// We must unlock the mutex before calling cleanunp, otherwise
		// browspr deadlocks.
		b.mu.Unlock()
		instance.cleanup()

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

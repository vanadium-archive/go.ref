// Browspr is the browser version of WSPR, intended to communicate with javascript through postMessage.
package browspr

import (
	"fmt"
	"sync"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/vdl"
	"veyron.io/veyron/veyron2/vdl/valconv"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/wspr/veyron/services/wsprd/account"
	"veyron.io/wspr/veyron/services/wsprd/lib"
	"veyron.io/wspr/veyron/services/wsprd/namespace"
	"veyron.io/wspr/veyron/services/wsprd/principal"
)

func init() {
	namespace.EpFormatter = lib.EndpointsToWs
}

// Browspr is an intermediary between our javascript code and the veyron
// network that allows our javascript library to use veyron.
type Browspr struct {
	rt               veyron2.Runtime
	profileFactory   func() veyron2.Profile
	listenSpec       *ipc.ListenSpec
	namespaceRoots   []string
	logger           vlog.Logger
	accountManager   *account.AccountManager
	postMessage      func(instanceId int32, ty, msg string)
	principalManager *principal.PrincipalManager

	// Locks activeInstances
	mu              sync.Mutex
	activeInstances map[int32]*pipe
}

// Create a new Browspr instance.
func NewBrowspr(runtime veyron2.Runtime,
	postMessage func(instanceId int32, ty, msg string),
	profileFactory func() veyron2.Profile,
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
		profileFactory:  profileFactory,
		listenSpec:      listenSpec,
		namespaceRoots:  wsNamespaceRoots,
		postMessage:     postMessage,
		rt:              runtime,
		logger:          runtime.Logger(),
		activeInstances: make(map[int32]*pipe),
	}

	// TODO(nlacasse, bjornick) use a serializer that can actually persist.
	var err error
	if browspr.principalManager, err = principal.NewPrincipalManager(runtime.Principal(), &principal.InMemorySerializer{}); err != nil {
		vlog.Fatalf("principal.NewPrincipalManager failed: %s", err)
	}

	browspr.accountManager = account.NewAccountManager(runtime, identd, browspr.principalManager)

	return browspr
}

func (browspr *Browspr) Shutdown() {
	// TODO(ataly, bprosnitz): Get rid of this method if possible.
}

// HandleMessage handles most messages from javascript and forwards them to a
// Controller.
func (b *Browspr) HandleMessage(instanceId int32, origin, msg string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	instance, ok := b.activeInstances[instanceId]
	if !ok {
		instance = newPipe(b, instanceId, origin)
		if instance == nil {
			return fmt.Errorf("Could not create pipe for origin %v: origin")
		}
		b.activeInstances[instanceId] = instance
	}

	return instance.handleMessage(msg)
}

type cleanupMessage struct {
	InstanceId int32
}

// HandleCleanupRpc cleans up the specified instance state. (For instance,
// when a browser tab is closed)
func (b *Browspr) HandleCleanupRpc(val *vdl.Value) (interface{}, error) {
	var msg cleanupMessage
	if err := valconv.Convert(&msg, val); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if instance, ok := b.activeInstances[msg.InstanceId]; ok {
		delete(b.activeInstances, msg.InstanceId)
		// NOTE(nlacasse): Calling cleanup() on the main thread locks
		// browspr, so we must do it in a goroutine.
		// TODO(nlacasse): Consider running all the message handlers in
		// goroutines.
		go instance.cleanup()
	}
	return nil, nil
}

type createAccountMessage struct {
	Token string
}

// Handler for creating an account in the principal manager.
// A valid OAuth2 access token must be supplied in the request body,
// which is exchanged for blessings from the veyron blessing server.
// An account based on the blessings is then added to WSPR's principal
// manager, and the set of blessing strings are returned to the client.
func (b *Browspr) HandleAuthCreateAccountRpc(val *vdl.Value) (interface{}, error) {
	var msg createAccountMessage
	if err := valconv.Convert(&msg, val); err != nil {
		return nil, err
	}

	account, err := b.accountManager.CreateAccount(msg.Token)
	if err != nil {
		return nil, err
	}

	return account, nil
}

type associateAccountMessage struct {
	Account string
	Origin  string
	Caveats []account.Caveat
}

// HandleAssociateAccountMessage associates an account with the specified origin.
func (b *Browspr) HandleAuthAssociateAccountRpc(val *vdl.Value) (interface{}, error) {
	var msg associateAccountMessage
	if err := valconv.Convert(&msg, val); err != nil {
		return nil, err
	}

	if err := b.accountManager.AssociateAccount(msg.Origin, msg.Account, msg.Caveats); err != nil {
		return nil, err
	}
	return nil, nil
}

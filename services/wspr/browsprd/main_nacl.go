// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Daemon browsprd implements the wspr web socket proxy as a Native Client
// executable, to be run as a Chrome extension.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/base64"
	"fmt"
	"runtime"
	"runtime/ppapi"

	"v.io/v23"
	"v.io/v23/logging"
	"v.io/v23/security"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/internal/logger"
	vsecurity "v.io/x/ref/lib/security"
	_ "v.io/x/ref/runtime/factories/chrome"
	"v.io/x/ref/runtime/protocols/lib/websocket"
	"v.io/x/ref/services/wspr/internal/app"
	"v.io/x/ref/services/wspr/internal/browspr"
	"v.io/x/ref/services/wspr/internal/channel/channel_nacl"
	"v.io/x/ref/services/wspr/internal/principal"
	"v.io/x/ref/services/wspr/internal/rpc/server"
)

const (
	browsprDir        = "/browspr/data"
	browsprKeyFile    = browsprDir + "/privateKey.pem."
	blessingRootsData = browsprDir + "/blessingroots.data"
	blessingRootsSig  = browsprDir + "/blessingroots.sig"
	blessingStoreData = browsprDir + "/blessingstore.data"
	blessingStoreSig  = browsprDir + "/blessingstore.sig"
)

func main() {
	security.OverrideCaveatValidation(server.CaveatValidation)
	ppapi.Init(newBrowsprInstance)
}

// browsprInstance represents an instance of a PPAPI client and receives
// callbacks from PPAPI to handle events.
type browsprInstance struct {
	ppapi.Instance
	fs      ppapi.FileSystem
	browspr *browspr.Browspr
	channel *channel_nacl.Channel
	logger  logging.Logger
}

var _ ppapi.InstanceHandlers = (*browsprInstance)(nil)

func newBrowsprInstance(inst ppapi.Instance) ppapi.InstanceHandlers {
	runtime.GOMAXPROCS(4)
	browsprInst := &browsprInstance{Instance: inst, logger: logger.Global()}
	browsprInst.initFileSystem()

	// Give the websocket interface the ppapi instance.
	websocket.PpapiInstance = inst

	// Set up the channel and register start rpc handler.
	browsprInst.channel = channel_nacl.NewChannel(inst)
	browsprInst.channel.RegisterRequestHandler("start", browsprInst.HandleStartMessage)

	return browsprInst
}

func (inst *browsprInstance) initFileSystem() {
	var err error
	// Create a filesystem.
	if inst.fs, err = inst.CreateFileSystem(ppapi.PP_FILESYSTEMTYPE_LOCALPERSISTENT); err != nil {
		panic(err.Error())
	}
	if ty := inst.fs.Type(); ty != ppapi.PP_FILESYSTEMTYPE_LOCALPERSISTENT {
		panic(fmt.Errorf("unexpected filesystem type: %d", ty))
	}
	// Open filesystem with expected size of 2K
	if err = inst.fs.OpenFS(1 << 11); err != nil {
		panic(fmt.Errorf("failed to open filesystem:%s", err))
	}
	// Create directory to store browspr keys
	if err = inst.fs.MkdirAll(browsprDir); err != nil {
		panic(fmt.Errorf("failed to create directory:%s", err))
	}
}

func (inst *browsprInstance) loadKeyFromStorage(browsprKeyFile string) (*ecdsa.PrivateKey, error) {
	inst.logger.VI(1).Infof("Attempting to read key from file %v", browsprKeyFile)

	rFile, err := inst.fs.Open(browsprKeyFile)
	if err != nil {
		inst.logger.VI(1).Infof("Key not found in file %v", browsprKeyFile)
		return nil, err
	}

	inst.logger.VI(1).Infof("Attempting to load cached browspr ecdsaPrivateKey in file %v", browsprKeyFile)
	defer rFile.Release()
	key, err := vsecurity.LoadPEMKey(rFile, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load browspr key:%s", err)
	}
	if ecdsaKey, ok := key.(*ecdsa.PrivateKey); !ok {
		return nil, fmt.Errorf("got key of type %T, want *ecdsa.PrivateKey", key)
	} else {
		return ecdsaKey, nil
	}
}

// Loads a saved key if one exists, otherwise creates a new one and persists it.
func (inst *browsprInstance) initKey() (*ecdsa.PrivateKey, error) {
	if ecdsaKey, err := inst.loadKeyFromStorage(browsprKeyFile); err == nil {
		return ecdsaKey, nil
	} else {
		inst.logger.VI(1).Infof("inst.loadKeyFromStorage(%v) failed: %v", browsprKeyFile, err)
	}

	inst.logger.VI(1).Infof("Generating new browspr ecdsaPrivateKey")

	// Generate new keys and store them.
	var ecdsaKey *ecdsa.PrivateKey
	var err error
	if _, ecdsaKey, err = vsecurity.NewPrincipalKey(); err != nil {
		return nil, fmt.Errorf("failed to generate security key:%s", err)
	}
	// Persist the keys in a local file.
	wFile, err := inst.fs.Create(browsprKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create file to persist browspr keys:%s", err)
	}
	defer wFile.Release()
	var b bytes.Buffer
	if err = vsecurity.SavePEMKey(&b, ecdsaKey, nil); err != nil {
		return nil, fmt.Errorf("failed to save browspr key:%s", err)
	}
	if n, err := wFile.Write(b.Bytes()); n != b.Len() || err != nil {
		return nil, fmt.Errorf("failed to write browspr key:%s", err)
	}
	return ecdsaKey, nil
}

func (inst *browsprInstance) newPrincipal(ecdsaKey *ecdsa.PrivateKey, blessingRootsData, blessingRootsSig, blessingStoreData, blessingStoreSig string) (security.Principal, error) {
	roots, err := principal.NewFileSerializer(blessingRootsData, blessingRootsSig, inst.fs)
	if err != nil {
		return nil, fmt.Errorf("failed to create blessing roots serializer:%s", err)
	}
	store, err := principal.NewFileSerializer(blessingStoreData, blessingStoreSig, inst.fs)
	if err != nil {
		return nil, fmt.Errorf("failed to create blessing store serializer:%s", err)
	}
	state := &vsecurity.PrincipalStateSerializer{
		BlessingRoots: roots,
		BlessingStore: store,
	}
	return vsecurity.NewPrincipalFromSigner(security.NewInMemoryECDSASigner(ecdsaKey), state)
}

func (inst *browsprInstance) newPersistentPrincipal(peerNames []string) (security.Principal, error) {
	ecdsaKey, err := inst.initKey()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ecdsa key:%s", err)
	}

	principal, err := inst.newPrincipal(ecdsaKey, blessingRootsData, blessingRootsSig, blessingStoreData, blessingStoreSig)
	if err != nil {
		inst.logger.VI(1).Infof("inst.newPrincipal(%v, %v, %v, %v, %v) failed: %v", ecdsaKey, blessingRootsData, blessingRootsSig, blessingStoreData, blessingStoreSig)

		// Delete the files and try again.
		if err := inst.cleanupBlessings(); err != nil {
			return nil, err
		}
		principal, err = inst.newPrincipal(ecdsaKey, blessingRootsData, blessingRootsSig, blessingStoreData, blessingStoreSig)
	}
	return principal, err
}

// cleanupBlessings removes the persisted blessing roots and store.
func (inst *browsprInstance) cleanupBlessings() error {
	inst.logger.VI(1).Info("Cleaning up blessing roots and store.")
	for _, file := range []string{blessingRootsData, blessingRootsSig, blessingStoreData, blessingStoreSig} {
		if err := inst.fs.Remove(file); err != nil {
			return fmt.Errorf("fs.Remove(%s) failed: %v", file, err)
		}
	}
	return nil
}

// Base64-decode and unmarshal a public key.
func decodeAndUnmarshalPublicKey(k string) (security.PublicKey, error) {
	decodedK, err := base64.URLEncoding.DecodeString(k)
	if err != nil {
		return nil, err
	}
	return security.UnmarshalPublicKey(decodedK)
}

func (inst *browsprInstance) HandleStartMessage(val *vom.RawBytes) (*vom.RawBytes, error) {
	inst.logger.VI(1).Info("Starting Browspr")
	var msg browspr.StartMessage
	if err := val.ToValue(&msg); err != nil {
		return nil, fmt.Errorf("HandleStartMessage did not receive StartMessage, received: %v, %v", val, err)
	}

	// The extension starts the nacl plugin with CleanupBlessings=true when it
	// detects it has been upgraded.  This prevents the plugin from breaking if
	// non-backwards-compatible changes have been made to the format of
	// blessings or roots.
	if msg.CleanupBlessings {
		if err := inst.cleanupBlessings(); err != nil {
			return nil, err
		}
	}

	p, err := inst.newPersistentPrincipal(msg.IdentitydBlessingRoot.Names)
	if err != nil {
		return nil, err
	}

	blessingName := "browspr-default-blessing"
	blessing, err := p.BlessSelf(blessingName)
	if err != nil {
		return nil, fmt.Errorf("p.BlessSelf(%v) failed: %v", blessingName, err)
	}

	// If msg.IdentitydBlessingRoot has a public key and names, then add
	// the public key to our set of trusted roots, and limit our blessing
	// to only talk to those names.
	if msg.IdentitydBlessingRoot.PublicKey != "" {
		if len(msg.IdentitydBlessingRoot.Names) == 0 {
			return nil, fmt.Errorf("invalid IdentitydBlessingRoot: Names is empty")
		}

		inst.logger.VI(1).Infof("Using blessing roots for identity with key %v and names %v", msg.IdentitydBlessingRoot.PublicKey, msg.IdentitydBlessingRoot.Names)
		keybytes, err := base64.URLEncoding.DecodeString(msg.IdentitydBlessingRoot.PublicKey)
		if err != nil {
			inst.logger.Fatalf("failed to decode public key (%v): %v", msg.IdentitydBlessingRoot.PublicKey, err)
		}

		for _, name := range msg.IdentitydBlessingRoot.Names {
			pattern := security.BlessingPattern(name)

			// Trust the identity servers blessing root.
			p.Roots().Add(keybytes, pattern)

			// Use our blessing to only talk to the identity server.
			if _, err := p.BlessingStore().Set(blessing, pattern); err != nil {
				return nil, fmt.Errorf("p.BlessingStore().Set(%v, %v) failed: %v", blessing, pattern, err)
			}
		}
	} else {
		inst.logger.VI(1).Infof("IdentitydBlessingRoot.PublicKey is empty.  Will allow browspr blessing to be shareable with all principals.")
		// Set our blessing as shareable with all peers.
		if _, err := p.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
			return nil, fmt.Errorf("p.BlessingStore().Set(%v, %v) failed: %v", blessing, security.AllPrincipals, err)
		}
	}

	// Initialize the runtime.
	// TODO(suharshs,mattr): Should we worried about not shutting down here?
	ctx, _ := v23.Init()

	ctx, err = v23.WithPrincipal(ctx, p)
	if err != nil {
		return nil, err
	}

	// TODO(cnicolaou): provide a means of configuring logging that
	// doesn't depend on vlog - e.g. ConfigureFromArgs(args []string) to
	// pair with ConfigureFromFlags(). See v.io/i/556
	// Configure logger with level and module from start message.
	inst.logger.VI(1).Infof("Configuring vlog with v=%v, modulesSpec=%v", msg.LogLevel, msg.LogModule)
	moduleSpec := vlog.ModuleSpec{}
	moduleSpec.Set(msg.LogModule)
	if err := vlog.Log.Configure(vlog.OverridePriorConfiguration(true), vlog.Level(msg.LogLevel), moduleSpec); err != nil {
		return nil, err
	}

	// TODO(ataly, bprosnitz, caprita): The runtime MUST be cleaned up
	// after use. Figure out the appropriate place to add the Cleanup call.

	v23.GetNamespace(ctx).SetRoots(msg.NamespaceRoot)

	listenSpec := v23.GetListenSpec(ctx)
	listenSpec.Proxy = msg.Proxy

	principalSerializer, err := principal.NewFileSerializer(browsprDir+"/principalData", browsprDir+"/principalSignature", inst.fs)
	if err != nil {
		return nil, fmt.Errorf("principal.NewFileSerializer() failed: %v", err)
	}

	inst.logger.VI(1).Infof("Starting browspr with config: proxy=%q mounttable=%q identityd=%q identitydBlessingRoot=%q ", msg.Proxy, msg.NamespaceRoot, msg.Identityd, msg.IdentitydBlessingRoot)
	inst.browspr = browspr.NewBrowspr(ctx,
		inst.BrowsprOutgoingPostMessage,
		&listenSpec,
		msg.Identityd,
		[]string{msg.NamespaceRoot},
		principalSerializer)

	// Add the rpc handlers that depend on inst.browspr.
	inst.channel.RegisterRequestHandler("auth:create-account", inst.browspr.HandleAuthCreateAccountRpc)
	inst.channel.RegisterRequestHandler("auth:associate-account", inst.browspr.HandleAuthAssociateAccountRpc)
	inst.channel.RegisterRequestHandler("auth:get-accounts", inst.browspr.HandleAuthGetAccountsRpc)
	inst.channel.RegisterRequestHandler("auth:origin-has-account", inst.browspr.HandleAuthOriginHasAccountRpc)
	inst.channel.RegisterRequestHandler("create-instance", inst.browspr.HandleCreateInstanceRpc)
	inst.channel.RegisterRequestHandler("cleanup", inst.browspr.HandleCleanupRpc)

	return nil, nil
}

func (inst *browsprInstance) BrowsprOutgoingPostMessage(instanceId int32, ty string, message string) {
	if message == "" {
		// TODO(nlacasse,bprosnitz): VarFromString crashes if the
		// string is empty, so we must use a placeholder.
		message = "."
	}
	dict := ppapi.NewDictVar()
	instVar := ppapi.VarFromInt(instanceId)
	bodyVar := ppapi.VarFromString(message)
	tyVar := ppapi.VarFromString(ty)
	dict.DictionarySet("instanceId", instVar)
	dict.DictionarySet("type", tyVar)
	dict.DictionarySet("body", bodyVar)
	inst.PostMessage(dict)
	instVar.Release()
	bodyVar.Release()
	tyVar.Release()
	dict.Release()
}

// HandleBrowsprMessage handles one-way messages of the type "browsprMsg" by
// sending them to browspr's handler.
func (inst *browsprInstance) HandleBrowsprMessage(instanceId int32, origin string, varMsg ppapi.Var) error {
	msg, err := varToMessage(varMsg)
	if err != nil {
		return fmt.Errorf("Invalid message: %v", err)
	}

	inst.logger.VI(1).Infof("Calling browspr's HandleMessage: instanceId %d origin %s message %s", instanceId, origin, msg)
	if err := inst.browspr.HandleMessage(instanceId, origin, msg); err != nil {
		return fmt.Errorf("Error while handling message in browspr: %v", err)
	}
	return nil
}

// HandleIntentionalPanic intentionally triggers a panic. This is used in tests
// of the extension's crash handling behavior.
// TODO(bprosnitz) We probably should conditionally compile this in via build
// tags so we don't hit it in production code.
func (inst *browsprInstance) HandleIntentionalPanic(instanceId int32, origin string, message ppapi.Var) error {
	// NOTE(nlacasse): Calling panic directly (not inside a goroutine)
	// sometimes blocks during the panic, causing the plugin to not emit a
	// "crash" event. I'm not sure why this happens, but it breaks the
	// test-nacl-plugin-crash test. Calling panic from within a goroutine seems
	// to reliably panic "all the way" and emit a "crash" event.
	go panic("Crashing intentionally")
	return nil
}

// HandleBrowsprRpc handles two-way rpc messages of the type "browsprRpc"
// sending them to the channel's handler.
func (inst *browsprInstance) HandleBrowsprRpc(instanceId int32, origin string, message ppapi.Var) error {
	inst.logger.VI(1).Infof("Got to HandleBrowsprRpc: instanceId: %d origin %s", instanceId, origin)
	inst.channel.HandleMessage(message)
	return nil
}

// handleGoError handles error returned by go code.
func (inst *browsprInstance) handleGoError(err error) {
	inst.logger.VI(2).Info(err)
	inst.LogString(ppapi.PP_LOGLEVEL_ERROR, fmt.Sprintf("Error in go code: %v", err.Error()))
	inst.logger.Error(err)
}

// HandleMessage receives messages from Javascript and uses them to perform actions.
// A message is of the form {"type": "typeName", "body": { stuff here }},
// where the body is passed to the message handler.
func (inst *browsprInstance) HandleMessage(message ppapi.Var) {
	inst.logger.VI(2).Infof("Got to HandleMessage")
	instanceId, err := message.LookupIntValuedKey("instanceId")
	if err != nil {
		inst.handleGoError(err)
		return
	}
	origin, err := message.LookupStringValuedKey("origin")
	if err != nil {
		inst.handleGoError(err)
		return
	}
	ty, err := message.LookupStringValuedKey("type")
	if err != nil {
		inst.handleGoError(err)
		return
	}
	var messageHandlers = map[string]func(int32, string, ppapi.Var) error{
		"browsprMsg":         inst.HandleBrowsprMessage,
		"browsprRpc":         inst.HandleBrowsprRpc,
		"intentionallyPanic": inst.HandleIntentionalPanic,
	}
	h, ok := messageHandlers[ty]
	if !ok {
		inst.handleGoError(fmt.Errorf("No handler found for message type: %q", ty))
		return
	}
	body, err := message.LookupKey("body")
	if err != nil {
		body = ppapi.VarUndefined
	}
	err = h(int32(instanceId), origin, body)
	body.Release()
	if err != nil {
		inst.handleGoError(err)
	}
}

func (inst browsprInstance) DidCreate(args map[string]string) bool {
	inst.logger.VI(2).Infof("Got to DidCreate")
	return true
}

func (inst *browsprInstance) DidDestroy() {
	inst.logger.VI(2).Infof("Got to DidDestroy()")
}

func (inst *browsprInstance) DidChangeView(view ppapi.View) {
	inst.logger.VI(2).Infof("Got to DidChangeView(%v)", view)
}

func (inst *browsprInstance) DidChangeFocus(has_focus bool) {
	inst.logger.VI(2).Infof("Got to DidChangeFocus(%v)", has_focus)
}

func (inst *browsprInstance) HandleDocumentLoad(url_loader ppapi.Resource) bool {
	inst.logger.VI(2).Infof("Got to HandleDocumentLoad(%v)", url_loader)
	return true
}

func (inst *browsprInstance) HandleInputEvent(event ppapi.InputEvent) bool {
	inst.logger.VI(2).Infof("Got to HandleInputEvent(%v)", event)
	return true
}

func (inst *browsprInstance) Graphics3DContextLost() {
	inst.logger.VI(2).Infof("Got to Graphics3DContextLost()")
}

func (inst *browsprInstance) MouseLockLost() {
	inst.logger.VI(2).Infof("Got to MouseLockLost()")
}

func varToMessage(v ppapi.Var) (app.Message, error) {
	var msg app.Message
	id, err := v.LookupIntValuedKey("id")
	if err != nil {
		return msg, err
	}
	ty, err := v.LookupIntValuedKey("type")
	if err != nil {
		return msg, err
	}
	data, err := v.LookupStringValuedKey("data")
	if err != nil {
		// OK for message to have empty data.
		data = ""
	}

	msg.Id = int32(id)
	msg.Data = data
	msg.Type = app.MessageType(ty)
	return msg, nil
}

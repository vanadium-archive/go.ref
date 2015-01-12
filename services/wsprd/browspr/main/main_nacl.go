package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/base64"
	"fmt"
	"runtime/ppapi"

	"v.io/core/veyron/lib/websocket"
	"v.io/core/veyron/profiles/chrome"
	vsecurity "v.io/core/veyron/security"
	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/vdl"
	"v.io/core/veyron2/vdl/valconv"
	"v.io/core/veyron2/vlog"
	"v.io/wspr/veyron/services/wsprd/browspr"
	"v.io/wspr/veyron/services/wsprd/channel/channel_nacl"
	"v.io/wspr/veyron/services/wsprd/lib"
)

func main() {
	ppapi.Init(newBrowsprInstance)
}

// browsprInstance represents an instance of a PPAPI client and receives
// callbacks from PPAPI to handle events.
type browsprInstance struct {
	ppapi.Instance
	fs      ppapi.FileSystem
	browspr *browspr.Browspr
	channel *channel_nacl.Channel
}

var _ ppapi.InstanceHandlers = (*browsprInstance)(nil)

func newBrowsprInstance(inst ppapi.Instance) ppapi.InstanceHandlers {
	browsprInst := &browsprInstance{Instance: inst}
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

const browsprDir = "/browspr/data"

// Loads a saved key if one exists, otherwise creates a new one and persists it.
func (inst *browsprInstance) initKey() (*ecdsa.PrivateKey, error) {
	var ecdsaKey *ecdsa.PrivateKey
	browsprKeyFile := browsprDir + "/privateKey.pem."
	// See whether we have any cached keys for WSPR
	if rFile, err := inst.fs.Open(browsprKeyFile); err == nil {
		fmt.Print("Opening cached browspr ecdsaPrivateKey")
		defer rFile.Release()
		key, err := vsecurity.LoadPEMKey(rFile, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to load browspr key:%s", err)
		}
		var ok bool
		if ecdsaKey, ok = key.(*ecdsa.PrivateKey); !ok {
			return nil, fmt.Errorf("got key of type %T, want *ecdsa.PrivateKey", key)
		}
	} else {
		fmt.Print("Generating new browspr ecdsaPrivateKey")
		// Generate new keys and store them.
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
	}
	return ecdsaKey, nil
}

func (inst *browsprInstance) newPersistantPrincipal(peerNames []string) (security.Principal, error) {
	ecdsaKey, err := inst.initKey()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ecdsa key:%s", err)
	}

	roots, err := browspr.NewFileSerializer(browsprDir+"/blessingroots.data", browsprDir+"/blessingroots.sig", inst.fs)
	if err != nil {
		return nil, fmt.Errorf("failed to create blessing roots serializer:%s", err)
	}
	store, err := browspr.NewFileSerializer(browsprDir+"/blessingstore.data", browsprDir+"/blessingstore.sig", inst.fs)
	if err != nil {
		return nil, fmt.Errorf("failed to create blessing store serializer:%s", err)
	}
	state := &vsecurity.PrincipalStateSerializer{
		BlessingRoots: roots,
		BlessingStore: store,
	}

	return vsecurity.NewPrincipalFromSigner(security.NewInMemoryECDSASigner(ecdsaKey), state)
}

type startMessage struct {
	Identityd             string
	IdentitydBlessingRoot blessingRoot
	Proxy                 string
	NamespaceRoot         string
}

// Copied from
// v.io/core/veyron/services/identity/handlers/blessing_root.go, since
// depcop prohibits importing that package.
type blessingRoot struct {
	Names     []string `json:"names"`
	PublicKey string   `json:"publicKey"`
}

// Base64-decode and unmarshal a public key.
func decodeAndUnmarshalPublicKey(k string) (security.PublicKey, error) {
	decodedK, err := base64.URLEncoding.DecodeString(k)
	if err != nil {
		return nil, err
	}
	return security.UnmarshalPublicKey(decodedK)
}

func (inst *browsprInstance) HandleStartMessage(val *vdl.Value) (interface{}, error) {
	fmt.Println("Starting Browspr")

	var msg startMessage
	if err := valconv.Convert(&msg, val); err != nil {
		return nil, err
	}

	listenSpec := ipc.ListenSpec{
		Proxy: msg.Proxy,
		Addrs: ipc.ListenAddrs{{Protocol: "ws", Address: ""}},
	}

	principal, err := inst.newPersistantPrincipal(msg.IdentitydBlessingRoot.Names)
	if err != nil {
		return nil, err
	}

	blessingName := "browspr-default-blessing"
	blessing, err := principal.BlessSelf(blessingName)
	if err != nil {
		return nil, fmt.Errorf("principal.BlessSelf(%v) failed: %v", blessingName, err)
	}

	// If msg.IdentitydBlessingRoot has a public key and names, then add
	// the public key to our set of trusted roots, and limit our blessing
	// to only talk to those names.
	if msg.IdentitydBlessingRoot.PublicKey != "" {
		if len(msg.IdentitydBlessingRoot.Names) == 0 {
			return nil, fmt.Errorf("invalid IdentitydBlessingRoot: Names is empty")
		}

		fmt.Printf("Using blessing roots for identity with key %v and names %v", msg.IdentitydBlessingRoot.PublicKey, msg.IdentitydBlessingRoot.Names)
		key, err := decodeAndUnmarshalPublicKey(msg.IdentitydBlessingRoot.PublicKey)
		if err != nil {
			vlog.Fatalf("decodeAndUnmarshalPublicKey(%v) failed: %v", msg.IdentitydBlessingRoot.PublicKey, err)
		}

		for _, name := range msg.IdentitydBlessingRoot.Names {
			globPattern := security.BlessingPattern(name).MakeGlob()

			// Trust the identity servers blessing root.
			principal.Roots().Add(key, globPattern)

			// Use our blessing to only talk to the identity server.
			if _, err := principal.BlessingStore().Set(blessing, globPattern); err != nil {
				return nil, fmt.Errorf("principal.BlessingStore().Set(%v, %v) failed: %v", blessing, globPattern, err)
			}
		}
	} else {
		fmt.Printf("IdentitydBlessingRoot.PublicKey is empty.  Will allow browspr blessing to be shareable with all principals.")
		// Set our blessing as shareable with all peers.
		if _, err := principal.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
			return nil, fmt.Errorf("principal.BlessingStore().Set(%v, %v) failed: %v", blessing, security.AllPrincipals, err)
		}
	}

	runtime, err := rt.New(options.RuntimePrincipal{principal})
	if err != nil {
		return nil, err
	}
	ctx := runtime.NewContext()

	// TODO(ataly, bprosnitz, caprita): The runtime MUST be cleaned up
	// after use. Figure out the appropriate place to add the Cleanup call.
	wsNamespaceRoots, err := lib.EndpointsToWs([]string{msg.NamespaceRoot})
	if err != nil {
		return nil, err
	}
	veyron2.GetNamespace(ctx).SetRoots(wsNamespaceRoots...)

	fmt.Printf("Starting browspr with config: proxy=%q mounttable=%q identityd=%q identitydBlessingRoot=%q ", msg.Proxy, msg.NamespaceRoot, msg.Identityd, msg.IdentitydBlessingRoot)
	inst.browspr = browspr.NewBrowspr(ctx,
		inst.BrowsprOutgoingPostMessage,
		chrome.New,
		&listenSpec,
		msg.Identityd,
		wsNamespaceRoots)

	// Add the rpc handlers that depend on inst.browspr.
	inst.channel.RegisterRequestHandler("auth:create-account", inst.browspr.HandleAuthCreateAccountRpc)
	inst.channel.RegisterRequestHandler("auth:associate-account", inst.browspr.HandleAuthAssociateAccountRpc)
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
func (inst *browsprInstance) HandleBrowsprMessage(instanceId int32, origin string, message ppapi.Var) error {
	str, err := message.AsString()
	if err != nil {
		// TODO(bprosnitz) Remove. We shouldn't panic on user input.
		return fmt.Errorf("Error while converting message to string: %v", err)
	}

	if err := inst.browspr.HandleMessage(instanceId, origin, str); err != nil {
		// TODO(bprosnitz) Remove. We shouldn't panic on user input.
		return fmt.Errorf("Error while handling message in browspr: %v", err)
	}
	return nil
}

// HandleBrowsprRpc handles two-way rpc messages of the type "browsprRpc"
// sending them to the channel's handler.
func (inst *browsprInstance) HandleBrowsprRpc(instanceId int32, origin string, message ppapi.Var) error {
	inst.channel.HandleMessage(message)
	return nil
}

// handleGoError handles error returned by go code.
func (inst *browsprInstance) handleGoError(err error) {
	vlog.VI(2).Info(err)
	inst.LogString(ppapi.PP_LOGLEVEL_ERROR, fmt.Sprintf("Error in go code: %v", err.Error()))
	vlog.Error(err)
}

// HandleMessage receives messages from Javascript and uses them to perform actions.
// A message is of the form {"type": "typeName", "body": { stuff here }},
// where the body is passed to the message handler.
func (inst *browsprInstance) HandleMessage(message ppapi.Var) {
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
		"browsprMsg": inst.HandleBrowsprMessage,
		"browsprRpc": inst.HandleBrowsprRpc,
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
	fmt.Printf("Got to DidCreate")
	return true
}

func (*browsprInstance) DidDestroy() {
	fmt.Printf("Got to DidDestroy()")
}

func (*browsprInstance) DidChangeView(view ppapi.View) {
	fmt.Printf("Got to DidChangeView(%v)", view)
}

func (*browsprInstance) DidChangeFocus(has_focus bool) {
	fmt.Printf("Got to DidChangeFocus(%v)", has_focus)
}

func (*browsprInstance) HandleDocumentLoad(url_loader ppapi.Resource) bool {
	fmt.Printf("Got to HandleDocumentLoad(%v)", url_loader)
	return true
}

func (*browsprInstance) HandleInputEvent(event ppapi.InputEvent) bool {
	fmt.Printf("Got to HandleInputEvent(%v)", event)
	return true
}

func (*browsprInstance) Graphics3DContextLost() {
	fmt.Printf("Got to Graphics3DContextLost()")
}

func (*browsprInstance) MouseLockLost() {
	fmt.Printf("Got to MouseLockLost()")
}

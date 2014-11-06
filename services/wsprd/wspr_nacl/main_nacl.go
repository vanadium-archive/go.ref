package main

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"runtime/ppapi"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/wspr/veyron/services/wsprd/wspr"

	_ "veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"
)

func main() {
	ppapi.Init(newWsprInstance)
}

// WSPR instance represents an instance on a PPAPI client and receives callbacks from PPAPI to handle events.
type wsprInstance struct {
	ppapi.Instance
	fs ppapi.FileSystem
}

var _ ppapi.InstanceHandlers = &wsprInstance{}

const wsprKeyDir = "/wspr/keys"

func (inst *wsprInstance) initFileSystem() {
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
	// Create directory to store wspr keys
	if err = inst.fs.MkdirAll(wsprKeyDir); err != nil {
		panic(fmt.Errorf("failed to create directory:%s", err))
	}
}

func (wsprInstance) DidCreate(args map[string]string) bool {
	fmt.Printf("Got to DidCreate")
	return true
}

func (wsprInstance) DidDestroy() {
	fmt.Printf("Got to DidDestroy()")
}

func (wsprInstance) DidChangeView(view ppapi.View) {
	fmt.Printf("Got to DidChangeView(%v)", view)
}

func (wsprInstance) DidChangeFocus(has_focus bool) {
	fmt.Printf("Got to DidChangeFocus(%v)", has_focus)
}

func (wsprInstance) HandleDocumentLoad(url_loader ppapi.Resource) bool {
	fmt.Printf("Got to HandleDocumentLoad(%v)", url_loader)
	return true
}

func (wsprInstance) HandleInputEvent(event ppapi.InputEvent) bool {
	fmt.Printf("Got to HandleInputEvent(%v)", event)
	return true
}

func (wsprInstance) Graphics3DContextLost() {
	fmt.Printf("Got to Graphics3DContextLost()")
}

// StartWSPR handles starting WSPR.
func (inst *wsprInstance) StartWSPR(message ppapi.Var) {
	var ecdsaKey *ecdsa.PrivateKey
	wsprKeyFile := wsprKeyDir + "/privateKey.pem."

	// See whether we have any cached keys for WSPR
	if rFile, err := inst.fs.Open(wsprKeyFile); err == nil {
		fmt.Print("Opening cached wspr ecdsaPrivateKey")
		defer rFile.Release()
		key, err := vsecurity.LoadPEMKey(rFile, nil)
		if err != nil {
			panic(fmt.Errorf("failed to load wspr key:%s", err))
		}
		var ok bool
		if ecdsaKey, ok = key.(*ecdsa.PrivateKey); !ok {
			panic(fmt.Errorf("got key of type %T, want *ecdsa.PrivateKey", key))
		}
	} else {
		if pemKey, err := message.LookupStringValuedKey("pemPrivateKey"); err == nil {
			fmt.Print("Using ecdsaPrivateKey from incoming request")
			key, err := vsecurity.LoadPEMKey(bytes.NewBufferString(pemKey), nil)
			if err != nil {
				panic(err.Error())
			}
			var ok bool
			ecdsaKey, ok = key.(*ecdsa.PrivateKey)
			if !ok {
				panic(fmt.Errorf("got key of type %T, want *ecdsa.PrivateKey", key))
			}
		} else {
			fmt.Print("Generating new wspr ecdsaPrivateKey")
			// Generate new keys and store them.
			var err error
			if _, ecdsaKey, err = vsecurity.NewKey(); err != nil {
				panic(fmt.Errorf("failed to generate security key:%s", err))
			}
		}
		// Persist the keys in a local file.
		wFile, err := inst.fs.Create(wsprKeyFile)
		if err != nil {
			panic(fmt.Errorf("failed to create file to persist wspr keys:%s", err))
		}
		defer wFile.Release()
		var b bytes.Buffer
		if err = vsecurity.SavePEMKey(&b, ecdsaKey, nil); err != nil {
			panic(fmt.Errorf("failed to save wspr key:%s", err))
		}
		if n, err := wFile.Write(b.Bytes()); n != b.Len() || err != nil {
			panic(fmt.Errorf("failed to write wspr key:%s", err))
		}
	}

	principal, err := vsecurity.NewPrincipalFromSigner(security.NewInMemoryECDSASigner(ecdsaKey))
	if err != nil {
		panic(err.Error())
	}

	defaultBlessingName, err := message.LookupStringValuedKey("defaultBlessingName")
	if err != nil {
		panic(err.Error())
	}

	if err := vsecurity.InitDefaultBlessings(principal, defaultBlessingName); err != nil {
		panic(err.Error())
	}
	runtime := rt.Init(options.RuntimePrincipal{principal})

	veyronProxy, err := message.LookupStringValuedKey("proxyName")
	if err != nil {
		panic(err.Error())
	}
	if veyronProxy == "" {
		panic("Empty proxy")
	}

	mounttable, err := message.LookupStringValuedKey("namespaceRoot")
	if err != nil {
		panic(err.Error())
	}
	runtime.Namespace().SetRoots(mounttable)

	identd, err := message.LookupStringValuedKey("identityd")
	if err != nil {
		panic(err.Error())
	}

	wsprHttpPort, err := message.LookupIntValuedKey("wsprHttpPort")
	if err != nil {
		panic(err.Error())
	}

	// TODO(cnicolaou,bprosnitz) Should we use the roaming profile?
	// It uses flags. We should change that.
	listenSpec := ipc.ListenSpec{
		Proxy:    veyronProxy,
		Protocol: "tcp",
		Address:  ":0",
	}

	fmt.Printf("Starting WSPR with config: proxy=%q mounttable=%q identityd=%q port=%d", veyronProxy, mounttable, identd, wsprHttpPort)
	proxy := wspr.NewWSPR(wsprHttpPort, listenSpec, identd, []string{mounttable}, options.RuntimePrincipal{principal})

	proxy.Listen()
	go func() {
		proxy.Serve()
	}()
}

// HandleMessage receives messages from Javascript and uses them to perform actions.
// A message is of the form {"type": "typeName", "body": { stuff here }},
// where the body is passed to the message handler.
func (inst *wsprInstance) HandleMessage(message ppapi.Var) {
	fmt.Printf("Entered HandleMessage\n")
	type handlerType func(ppapi.Var)
	handlerMap := map[string]handlerType{
		"start": inst.StartWSPR,
	}
	ty, err := message.LookupStringValuedKey("type")
	if err != nil {
		panic(err.Error())
	}
	h, ok := handlerMap[ty]
	if !ok {
		panic(fmt.Sprintf("No handler found for message type: %q", ty))
	}
	body, err := message.LookupKey("body")
	if err != nil {
		body = ppapi.VarFromString("INVALID")
	}
	h(body)
	body.Release()
}

func (wsprInstance) MouseLockLost() {
	fmt.Printf("Got to MouseLockLost()")
}

func newWsprInstance(inst ppapi.Instance) ppapi.InstanceHandlers {
	wspr := &wsprInstance{Instance: inst}
	wspr.initFileSystem()
	return wspr
}

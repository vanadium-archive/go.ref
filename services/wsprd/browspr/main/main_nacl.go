package main

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"io"
	"os"
	"runtime/ppapi"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/wspr/veyron/services/wsprd/browspr"

	_ "veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"
)

func main() {
	ppapi.Init(newBrowsprInstance)
}

// fileSerializer implements vsecurity.SerializerReaderWriter that persists state to
// files with the pepper API.
type fileSerializer struct {
	system    ppapi.FileSystem
	data      *ppapi.FileIO
	signature *ppapi.FileIO

	dataFile      string
	signatureFile string
}

func (fs *fileSerializer) Readers() (io.ReadCloser, io.ReadCloser, error) {
	if fs.data == nil || fs.signature == nil {
		return nil, nil, nil
	}
	return fs.data, fs.signature, nil
}

func (fs *fileSerializer) Writers() (io.WriteCloser, io.WriteCloser, error) {
	// Remove previous version of the files
	fs.system.Remove(fs.dataFile)
	fs.system.Remove(fs.signatureFile)
	var err error
	if fs.data, err = fs.system.Create(fs.dataFile); err != nil {
		return nil, nil, err
	}
	if fs.signature, err = fs.system.Create(fs.signatureFile); err != nil {
		return nil, nil, err
	}
	return fs.data, fs.signature, nil
}

func fileNotExist(err error) bool {
	pe, ok := err.(*os.PathError)
	return ok && pe.Err.Error() == "file not found"
}

func newFileSerializer(dataFile, signatureFile string, system ppapi.FileSystem) (*fileSerializer, error) {
	data, err := system.Open(dataFile)
	if err != nil && !fileNotExist(err) {
		return nil, err
	}
	signature, err := system.Open(signatureFile)
	if err != nil && !fileNotExist(err) {
		return nil, err
	}
	fmt.Print("NewFileSerializer:%v", err)
	return &fileSerializer{
		system:        system,
		data:          data,
		signature:     signature,
		dataFile:      dataFile,
		signatureFile: signatureFile,
	}, nil
}

// WSPR instance represents an instance of a PPAPI client and receives callbacks from PPAPI to handle events.
type browsprInstance struct {
	ppapi.Instance
	fs      ppapi.FileSystem
	browspr *browspr.Browspr
}

var _ ppapi.InstanceHandlers = (*browsprInstance)(nil)

const wsprDir = "/wspr/data"

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
	// Create directory to store wspr keys
	if err = inst.fs.MkdirAll(wsprDir); err != nil {
		panic(fmt.Errorf("failed to create directory:%s", err))
	}
}

func newBrowsprInstance(inst ppapi.Instance) ppapi.InstanceHandlers {
	bwspr := &browsprInstance{Instance: inst}
	bwspr.initFileSystem()
	return bwspr
}

// StartBrowspr handles starting browspr.
func (inst *browsprInstance) StartBrowspr(message ppapi.Var) error {
	var ecdsaKey *ecdsa.PrivateKey
	wsprKeyFile := wsprDir + "/privateKey.pem."

	// See whether we have any cached keys for WSPR
	if rFile, err := inst.fs.Open(wsprKeyFile); err == nil {
		fmt.Print("Opening cached wspr ecdsaPrivateKey")
		defer rFile.Release()
		key, err := vsecurity.LoadPEMKey(rFile, nil)
		if err != nil {
			return fmt.Errorf("failed to load wspr key:%s", err)
		}
		var ok bool
		if ecdsaKey, ok = key.(*ecdsa.PrivateKey); !ok {
			return fmt.Errorf("got key of type %T, want *ecdsa.PrivateKey", key)
		}
	} else {
		if pemKey, err := message.LookupStringValuedKey("pemPrivateKey"); err == nil {
			fmt.Print("Using ecdsaPrivateKey from incoming request")
			key, err := vsecurity.LoadPEMKey(bytes.NewBufferString(pemKey), nil)
			if err != nil {
				return err
			}
			var ok bool
			ecdsaKey, ok = key.(*ecdsa.PrivateKey)
			if !ok {
				return fmt.Errorf("got key of type %T, want *ecdsa.PrivateKey", key)
			}
		} else {
			fmt.Print("Generating new wspr ecdsaPrivateKey")
			// Generate new keys and store them.
			var err error
			if _, ecdsaKey, err = vsecurity.NewPrincipalKey(); err != nil {
				return fmt.Errorf("failed to generate security key:%s", err)
			}
		}
		// Persist the keys in a local file.
		wFile, err := inst.fs.Create(wsprKeyFile)
		if err != nil {
			return fmt.Errorf("failed to create file to persist wspr keys:%s", err)
		}
		defer wFile.Release()
		var b bytes.Buffer
		if err = vsecurity.SavePEMKey(&b, ecdsaKey, nil); err != nil {
			return fmt.Errorf("failed to save wspr key:%s", err)
		}
		if n, err := wFile.Write(b.Bytes()); n != b.Len() || err != nil {
			return fmt.Errorf("failed to write wspr key:%s", err)
		}
	}

	roots, err := newFileSerializer(wsprDir+"/blessingroots.data", wsprDir+"/blessingroots.sig", inst.fs)
	if err != nil {
		return fmt.Errorf("failed to create blessing roots serializer:%s", err)
	}
	store, err := newFileSerializer(wsprDir+"/blessingstore.data", wsprDir+"/blessingstore.sig", inst.fs)
	if err != nil {
		return fmt.Errorf("failed to create blessing store serializer:%s", err)
	}
	state := &vsecurity.PrincipalStateSerializer{
		BlessingRoots: roots,
		BlessingStore: store,
	}
	principal, err := vsecurity.NewPrincipalFromSigner(security.NewInMemoryECDSASigner(ecdsaKey), state)
	if err != nil {
		return err
	}

	defaultBlessingName, err := message.LookupStringValuedKey("defaultBlessingName")
	if err != nil {
		return err
	}

	if err := vsecurity.InitDefaultBlessings(principal, defaultBlessingName); err != nil {
		return err
	}
	runtime := rt.Init(options.RuntimePrincipal{principal})

	veyronProxy, err := message.LookupStringValuedKey("proxyName")
	if err != nil {
		return err
	}
	if veyronProxy == "" {
		return fmt.Errorf("proxyName field was empty")
	}

	mounttable, err := message.LookupStringValuedKey("namespaceRoot")
	if err != nil {
		return err
	}
	runtime.Namespace().SetRoots(mounttable)

	identd, err := message.LookupStringValuedKey("identityd")
	if err != nil {
		return err
	}

	// TODO(cnicolaou,bprosnitz) Should we use the roaming profile?
	// It uses flags. We should change that.
	listenSpec := ipc.ListenSpec{
		Proxy:    veyronProxy,
		Protocol: "tcp",
		Address:  ":0",
	}

	fmt.Printf("Starting browspr with config: proxy=%q mounttable=%q identityd=%q ", veyronProxy, mounttable, identd)
	inst.browspr = browspr.NewBrowspr(inst.BrowsprOutgoingPostMessage, listenSpec, identd, []string{mounttable}, options.RuntimePrincipal{principal})
	return nil
}

func (inst *browsprInstance) BrowsprOutgoingPostMessage(instanceId int32, ty string, message string) {
	dict := ppapi.NewDictVar()
	instVar := ppapi.VarFromInt(instanceId)
	msgVar := ppapi.VarFromString(message)
	tyVar := ppapi.VarFromString(ty)
	dict.DictionarySet("instanceId", instVar)
	dict.DictionarySet("type", tyVar)
	dict.DictionarySet("msg", msgVar)
	inst.PostMessage(dict)
	instVar.Release()
	msgVar.Release()
	tyVar.Release()
	dict.Release()
}

func (inst *browsprInstance) HandleBrowsprMessage(message ppapi.Var) error {
	instanceId, err := message.LookupIntValuedKey("instanceId")
	if err != nil {
		return err
	}

	msg, err := message.LookupStringValuedKey("msg")
	if err != nil {
		return err
	}

	if err := inst.browspr.HandleMessage(int32(instanceId), msg); err != nil {
		// TODO(bprosnitz) Remove. We shouldn't panic on user input.
		return fmt.Errorf("Error while handling message in browspr: %v", err)
	}
	return nil
}

func (inst *browsprInstance) HandleBrowsprCleanup(message ppapi.Var) error {
	instanceId, err := message.LookupIntValuedKey("instanceId")
	if err != nil {
		return err
	}

	inst.browspr.HandleCleanupMessage(int32(instanceId))
	return nil
}

func (inst *browsprInstance) HandleBrowsprCreateAccount(message ppapi.Var) error {
	instanceId, err := message.LookupIntValuedKey("instanceId")
	if err != nil {
		return err
	}

	accessToken, err := message.LookupStringValuedKey("accessToken")
	if err != nil {
		return err
	}

	err = inst.browspr.HandleCreateAccountMessage(int32(instanceId), accessToken)
	if err != nil {
		// TODO(bprosnitz) Remove. We shouldn't panic on user input.
		panic(fmt.Sprintf("Error creating account: %v", err))
	}
	return nil
}

func (inst *browsprInstance) HandleBrowsprAssociateAccount(message ppapi.Var) error {
	origin, err := message.LookupStringValuedKey("origin")
	if err != nil {
		return err
	}

	account, err := message.LookupStringValuedKey("account")
	if err != nil {
		return err
	}

	err = inst.browspr.HandleAssociateAccountMessage(origin, account)
	if err != nil {
		// TODO(bprosnitz) Remove. We shouldn't panic on user input.
		return fmt.Errorf("Error associating account: %v", err)
	}
	return nil
}

// handleGoError handles error returned by go code.
func (inst *browsprInstance) handleGoError(err error) {
	vlog.VI(2).Info(err)
	inst.LogString(ppapi.PP_LOGLEVEL_ERROR, fmt.Sprintf("Error in go code: %v", err.Error()))
}

// HandleMessage receives messages from Javascript and uses them to perform actions.
// A message is of the form {"type": "typeName", "body": { stuff here }},
// where the body is passed to the message handler.
func (inst *browsprInstance) HandleMessage(message ppapi.Var) {
	fmt.Printf("Entered HandleMessage")
	ty, err := message.LookupStringValuedKey("type")
	if err != nil {
		inst.handleGoError(err)
		return
	}
	var messageHandlers = map[string]func(ppapi.Var) error{
		"start":                   inst.StartBrowspr,
		"browsprMsg":              inst.HandleBrowsprMessage,
		"browpsrClose":            inst.HandleBrowsprCleanup,
		"browsprCreateAccount":    inst.HandleBrowsprCreateAccount,
		"browsprAssociateAccount": inst.HandleBrowsprAssociateAccount,
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
	err = h(body)
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

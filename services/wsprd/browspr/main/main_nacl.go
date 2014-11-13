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
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/wspr/veyron/services/wsprd/browspr"

	_ "veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"
)

func main() {
	ppapi.Init(newBrowsprInstance)
}

// WSPR instance represents an instance of a PPAPI client and receives callbacks from PPAPI to handle events.
type browsprInstance struct {
	ppapi.Instance
	browspr *browspr.Browspr
}

var _ ppapi.InstanceHandlers = (*browsprInstance)(nil)

func newBrowsprInstance(inst ppapi.Instance) ppapi.InstanceHandlers {
	return &browsprInstance{
		Instance: inst,
	}
}

// StartBrowspr handles starting browspr.
func (inst *browsprInstance) StartBrowspr(message ppapi.Var) error {
	// HACK!!
	// TODO(ataly, ashankar, bprosnitz): The private key should be
	// generated/retrieved by directly talking to some secure storage
	// in Chrome, e.g. LocalStorage (and not from the config as below).
	pemKey, err := message.LookupStringValuedKey("pemPrivateKey")
	if err != nil {
		return err
	}

	// TODO(ataly, ashankr, bprosnitz): Figure out whether we need
	// passphrase protection here (most likely we do but how do we
	// request the passphrase from the user?)
	key, err := vsecurity.LoadPEMKey(bytes.NewBufferString(pemKey), nil)
	if err != nil {
		return err
	}
	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return fmt.Errorf("got key of type %T, want *ecdsa.PrivateKey", key)
	}

	principal, err := vsecurity.NewPrincipalFromSigner(security.NewInMemoryECDSASigner(ecdsaKey), nil)
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

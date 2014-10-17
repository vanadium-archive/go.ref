package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"runtime/ppapi"
	"syscall"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/wspr/veyron/services/wsprd/wspr"

	// TODO(cnicolaou): shouldn't be depending on the runtime here.
	_ "veyron.io/veyron/veyron/profiles"
	_ "veyron.io/veyron/veyron/runtimes/google/security"
)

func main() {
	ppapi.Init(newWsprInstance)
}

// WSPR instance represents an instance on a PPAPI client and receives callbacks from PPAPI to handle events.
type wsprInstance struct {
	ppapi.Instance
}

var _ ppapi.InstanceHandlers = wsprInstance{}

func (inst wsprInstance) DidCreate(args map[string]string) bool {
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
func (wsprInstance) StartWSPR(message ppapi.Var) {
	identityContents, err := message.LookupStringValuedKey("identityContents")
	if err != nil {
		panic(err.Error())
	}
	file, err := ioutil.TempFile(os.TempDir(), "veyron_id")
	if err != nil {
		panic(err.Error())
	}
	_, err = file.WriteString(identityContents)
	if err != nil {
		panic(err.Error())
	}
	if err := file.Close(); err != nil {
		panic(err.Error())
	}
	f, err := os.Open(file.Name())
	if err != nil {
		panic(err.Error())
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("IDENTITY: %s", string(b))
	f.Close()
	syscall.Setenv("VEYRON_IDENTITY", file.Name())

	rt.Init()

	veyronProxy, err := message.LookupStringValuedKey("proxy")
	if err != nil {
		panic(err.Error())
	}
	if veyronProxy == "" {
		panic("Empty proxy")
	}

	mounttable, err := message.LookupStringValuedKey("mounttable")
	if err != nil {
		panic(err.Error())
	}
	syscall.Setenv("MOUNTTABLE_ROOT", mounttable)
	syscall.Setenv("NAMESPACE_ROOT", mounttable)

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
	proxy := wspr.NewWSPR(wsprHttpPort, listenSpec, identd)
	go func() {
		proxy.Run()
	}()
}

// HandleMessage receives messages from Javascript and uses them to perform actions.
// A message is of the form {"type": "typeName", "body": { stuff here }},
// where the body is passed to the message handler.
func (inst wsprInstance) HandleMessage(message ppapi.Var) {
	type handlerType func(ppapi.Var)
	handlerMap := map[string]handlerType{
		"start": inst.StartWSPR,
	}
	fmt.Printf("Got to HandleMessage(%v)", message)
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
	return wsprInstance{Instance: inst}
}

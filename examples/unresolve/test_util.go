package unresolve

import (
	"fmt"
	"testing"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	mtidl "veyron2/services/mounttable"
	"veyron2/vlog"

	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	mounttable "veyron/services/mounttable/lib"

	fortuneidl "veyron/examples/fortune"
)

func initRT(opts ...veyron2.ROpt) func() {
	return rt.Init(opts...).Shutdown
}

func newServer(opts ...ipc.ServerOpt) ipc.Server {
	server, err := rt.R().NewServer(opts...)
	if err != nil {
		panic(fmt.Sprintf("r.NewServer failed with %v", err))
	}
	return server
}

func createServer(server ipc.Server, prefix string, dispatcher ipc.Dispatcher) string {
	if err := server.Register(prefix, dispatcher); err != nil {
		panic(fmt.Sprintf("server.Register failed with %v", err))
	}
	ep, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("server.Listen failed with %v", err))
	}
	return naming.JoinAddressName(ep.String(), prefix)
}

func serverMain(servesMT bool, serviceCreator func(ipc.Server) string, args []string) {
	defer initRT()()
	server := newServer(veyron2.ServesMountTableOpt(servesMT))
	defer server.Stop()
	service := serviceCreator(server)
	vlog.Infof("created %v", service)
	for _, arg := range args {
		if err := server.Publish(arg); err != nil {
			panic(fmt.Sprintf("server.Publish(%q) failed with %v", arg, err))
		}
	}
	fmt.Println("ready")
	blackbox.WaitForEOFOnStdin()
}

func createMT(server ipc.Server) string {
	mt, err := mounttable.NewMountTable("")
	if err != nil {
		panic(fmt.Sprintf("NewMountTable failed with %v", err))
	}
	return createServer(server, "mt", mt)
}

func childMT(args []string) {
	serverMain(true, createMT, args)
}

func createMTClient(name string) mtidl.MountTable {
	client, err := mtidl.BindMountTable(name)
	if err != nil {
		panic(fmt.Sprintf("BindMountTable failed with %v", err))
	}
	return client
}

const fixedFortuneMessage = "Sooner than you think, you will be deeply dissatisfied with a fortune."

type fortune struct{}

func (*fortune) Get(ipc.ServerContext) (string, error) {
	return fixedFortuneMessage, nil
}

func (*fortune) Add(ipc.ServerContext, string) error {
	return nil
}

func createFortune(server ipc.Server) string {
	return createServer(server, "fortune", ipc.SoloDispatcher(fortuneidl.NewServerFortune(new(fortune)), nil))
}

func childFortune(args []string) {
	serverMain(false, createFortune, args)
}

type fortuneCustomUnresolve struct {
	custom string
}

func (*fortuneCustomUnresolve) Get(ipc.ServerContext) (string, error) {
	return fixedFortuneMessage, nil
}

func (*fortuneCustomUnresolve) Add(ipc.ServerContext, string) error {
	return nil
}

func (*fortuneCustomUnresolve) UnresolveStep(context ipc.ServerContext) ([]string, error) {
	servers, err := rt.R().MountTable().ResolveToMountTable(rt.R().NewContext(), "I/want/to/know")
	if err != nil {
		return nil, err
	}
	var reply []string
	for _, s := range servers {
		r := naming.MakeResolvable(s)
		reply = append(reply, naming.Join(r, "the/future"))
	}
	return reply, nil
}

func createFortuneCustomUnresolve(server ipc.Server) string {
	oa := createServer(server, "tell/me/the/future", ipc.SoloDispatcher(fortuneidl.NewServerFortune(new(fortuneCustomUnresolve)), nil))
	ep, _ := naming.SplitAddressName(oa)
	oa = naming.MakeTerminal(naming.JoinAddressName(ep, "tell/me"))
	// Doesn't get unmounted.  Fine for a test.
	oa = naming.MakeTerminal(oa)
	rt.R().MountTable().Mount(rt.R().NewContext(), "I/want/to/know", oa, 0)
	return oa
}

func childFortuneCustomUnresolve(args []string) {
	serverMain(false, createFortuneCustomUnresolve, args)
}

func createFortuneClient(rt veyron2.Runtime, name string) fortuneidl.Fortune {
	client, err := fortuneidl.BindFortune(name, veyron2.RuntimeOpt{rt})
	if err != nil {
		panic(fmt.Sprintf("BindFortune failed with %v", err))
	}
	return client
}

type fortuneNoIDL struct{}

func (*fortuneNoIDL) Get(ipc.ServerCall) (string, error) {
	return fixedFortuneMessage, nil
}

func (*fortuneNoIDL) UnresolveStep(ipc.ServerCall) ([]string, error) {
	servers, err := rt.R().MountTable().ResolveToMountTable(rt.R().NewContext(), "g")
	if err != nil {
		return nil, err
	}
	var reply []string
	for _, s := range servers {
		r := naming.MakeResolvable(s)
		reply = append(reply, naming.Join(r, "fortune"))
	}
	return reply, nil
}

func createFortuneNoIDL(server ipc.Server) string {
	return createServer(server, "fortune", ipc.SoloDispatcher(new(fortuneNoIDL), nil))
}

func childFortuneNoIDL(args []string) {
	serverMain(false, createFortuneNoIDL, args)
}

func resolveStep(t *testing.T, name string) string {
	client := createMTClient(name)
	results, suffix, err := client.ResolveStep(rt.R().NewContext())
	if err != nil {
		t.Errorf("ResolveStep on %q failed with %v", name, err)
		return ""
	}
	if len(results) != 1 {
		t.Errorf("Expected one result when resolving %q, got %q", name, results)
		return ""
	}
	return naming.Join(results[0].Server, suffix)
}

func resolve(t *testing.T, mt naming.MountTable, name string) string {
	results, err := mt.Resolve(rt.R().NewContext(), name)
	if err != nil {
		t.Errorf("Resolve failed with %v", err)
		return ""
	}
	if len(results) != 1 {
		t.Errorf("Expected one result when resolving %q, got %q", name, results)
		return ""
	}
	return results[0]
}

type unresolver interface {
	UnresolveStep(context.T, ...ipc.CallOpt) ([]string, error)
}

func unresolveStep(t *testing.T, ctx context.T, c unresolver) string {
	unres, err := c.UnresolveStep(ctx)
	if err != nil {
		t.Errorf("UnresolveStep failed with %v", err)
		return ""
	}
	if len(unres) != 1 {
		t.Errorf("c.UnresolveStep wanted 1 result, got: %q", unres)
		return ""
	}
	return unres[0]
}

func unresolve(t *testing.T, mt naming.MountTable, name string) string {
	results, err := mt.Unresolve(rt.R().NewContext(), name)
	if err != nil {
		t.Errorf("Unresolve failed with %v", err)
		return ""
	}
	if len(results) != 1 {
		t.Errorf("Expected one result when unresolving %q, got %q", name, results)
		return ""
	}
	return results[0]
}

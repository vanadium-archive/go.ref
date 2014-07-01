package unresolve

import (
	"fmt"
	"testing"

	"veyron2"
	"veyron2/context"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
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

func createServer(name string, dispatcher ipc.Dispatcher, opts ...ipc.ServerOpt) (ipc.Server, string) {
	server, err := rt.R().NewServer(opts...)
	if err != nil {
		panic(fmt.Sprintf("r.NewServer failed with %v", err))
	}
	ep, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("server.Listen failed with %v", err))
	}
	if err := server.Serve(name, dispatcher); err != nil {
		panic(fmt.Sprintf("server.Serve failed with %v", err))
	}
	oa := naming.JoinAddressName(ep.String(), "")
	vlog.Infof("created %s -> %s", name, oa)
	return server, oa
}

func childMT(args []string) {
	defer initRT()()
	for _, arg := range args {
		server, _ := createMTServer(arg)
		defer server.Stop()
	}
	fmt.Println("ready")
	blackbox.WaitForEOFOnStdin()
}

func createMTServer(mp string) (ipc.Server, string) {
	mt, err := mounttable.NewMountTable("")
	if err != nil {
		panic(fmt.Sprintf("NewMountTable failed with %v", err))
	}
	return createServer(mp, mt, veyron2.ServesMountTableOpt(true))
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

func childFortune(args []string) {
	defer initRT()()
	server, _ := createServer(args[0], ipc.SoloDispatcher(fortuneidl.NewServerFortune(new(fortune)), nil))
	defer server.Stop()
	for _, arg := range args[1:] {
		server.Serve(arg, nil)
	}
	fmt.Println("ready")
	blackbox.WaitForEOFOnStdin()
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
	servers, err := rt.R().Namespace().ResolveToMountTable(rt.R().NewContext(), "I/want/to/know")
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

// Can't use the soloDispatcher (which is a bad name in any case) since it
// doesn't allow name suffixes (which is also a silly restriction).
// TODO(cnicolaou): rework soloDispatcher.
type fortuned struct{ obj interface{} }

func (f *fortuned) Lookup(string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(f.obj), nil, nil
}

func createFortuneCustomUnresolve(mp string) (ipc.Server, string) {
	server, oa := createServer(mp, &fortuned{fortuneidl.NewServerFortune(new(fortuneCustomUnresolve))})
	rt.R().Namespace().Mount(rt.R().NewContext(), "I/want/to/know", oa+"//", 0)
	rt.R().Namespace().Mount(rt.R().NewContext(), "tell/me", oa+"//", 0)
	return server, oa
}

func childFortuneCustomUnresolve(args []string) {
	defer initRT()()
	for _, arg := range args {
		server, _ := createFortuneCustomUnresolve(arg)
		defer server.Stop()
	}
	fmt.Println("ready")
	blackbox.WaitForEOFOnStdin()
}

func childFortuneNoIDL(args []string) {
	defer initRT()()
	for _, arg := range args {
		server, _ := createServer(arg, ipc.SoloDispatcher(new(fortuneNoIDL), nil))
		defer server.Stop()
	}
	fmt.Println("ready")
	blackbox.WaitForEOFOnStdin()
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
	servers, err := rt.R().Namespace().ResolveToMountTable(rt.R().NewContext(), "g")
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

func resolve(t *testing.T, ns naming.Namespace, name string) string {
	results, err := ns.Resolve(rt.R().NewContext(), name)
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

func unresolve(t *testing.T, ns naming.Namespace, name string) string {
	results, err := ns.Unresolve(rt.R().NewContext(), name)
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

func glob(t *testing.T, pattern string) []string {
	var replies []string
	rc, err := rt.R().Namespace().Glob(rt.R().NewContext(), pattern)
	if err != nil {
		t.Errorf("Glob(%s): err %v", pattern, err)
		return nil
	}
	for s := range rc {
		replies = append(replies, s.Name)
	}
	return replies
}

package modules

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"veyron/lib/testutil/blackbox"
	mounttable "veyron/services/mounttable/lib"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

var (
	ErrUsage = errors.New("bad usage")
)

func init() {
	blackbox.CommandTable["rootMT"] = rootMTChild
	blackbox.CommandTable["nodeMT"] = leafMTChild
}

type mountTable struct {
	root bool
}

func NewRootMT() T {
	return &mountTable{root: true}
}

func NewNodeMT() T {
	return &mountTable{root: false}
}

func (*mountTable) Daemon() bool {
	return true
}

func (mt *mountTable) Help() string {
	if mt.root {
		return `<prefix>
run a root mounTable using Register(prefix...)`
	} else {
		return `<root> <prefix> <suffix>
run a mountTable with root as its root mountTable and using Register(prefix...) and Publish(suffix)`
	}
}

func (mt *mountTable) Run(args []string) (Variables, []string, Handle, error) {
	if (mt.root && len(args) != 1) || (!mt.root && len(args) != 3) {
		return nil, nil, nil, fmt.Errorf("wrong #args: %s", mt.Help())
	}
	name := map[bool]string{true: "rootMT", false: "nodeMT"}[mt.root]
	var env []string
	if !mt.root {
		env = append(env, "MOUNTTABLE_ROOT="+args[0])
		args = args[1:]
	}
	c, v, r, err := bbSpawn(name, args, env)
	if err != nil {
		defer c.Cleanup()
		return v, r, nil, err
	}
	h := &handle{c}
	children.add(h)
	return v, r, h, nil
}

func rootMTChild(args []string) {
	if len(args) != 1 {
		bbExitWithError("wrong #args")
	}
	serveMountTable(true, args)
}

func leafMTChild(args []string) {
	if len(args) != 2 {
		bbExitWithError("wrong #args")
	}
	serveMountTable(false, args)
}

func serveMountTable(root bool, args []string) {
	rt.Init()
	fmt.Println("ready")
	server, err := rt.R().NewServer(veyron2.ServesMountTableOpt(true))
	if err != nil {
		bbExitWithError(fmt.Sprintf("root failed: %v", err))
	}
	prefix := args[0]
	name, err := newMountTableServer(server, prefix)
	if err != nil {
		bbExitWithError(fmt.Sprintf("root failed: %v", err))
	}
	addr, _ := naming.SplitAddressName(name)
	if !root {
		if err := server.Publish(args[1]); err != nil {
			bbExitWithError(fmt.Sprintf("root failed: %v", err))
		}
	}
	fmt.Printf("MT_ADDR=%s\n", addr)
	fmt.Printf("MT_NAME=%s\n", name)
	fmt.Printf("PID=%d\n", os.Getpid())
	fmt.Println("running\n")
	blackbox.WaitForEOFOnStdin()
	fmt.Println("done\n")
}

func newMountTableServer(server ipc.Server, prefix string) (string, error) {
	mt, err := mounttable.NewMountTable("")
	if err != nil {
		return "", fmt.Errorf("NewMountTableServer failed with %v", err)
	}
	if err := server.Register(prefix, mt); err != nil {
		return "", fmt.Errorf("server.Register failed with %v", err)
	}
	ep, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("server.Listen failed with %v", err)
	}
	name := naming.JoinAddressName(ep.String(), prefix)
	vlog.Infof("Serving MountTable on %q", name)
	return name, nil
}

type setroot struct{}

func NewSetRoot() T {
	return &setroot{}
}

func (*setroot) Help() string {
	return `[<mountTable name>]+
set the local mountTable client's roots to supplied mountTable names`
}

func (*setroot) Daemon() bool { return false }

func (s *setroot) Run(args []string) (Variables, []string, Handle, error) {
	if len(args) == 0 {
		return nil, nil, nil, fmt.Errorf("wrong #args: %s", s.Help())
	}
	for _, r := range args {
		if !naming.Rooted(r) {
			return nil, nil, nil, fmt.Errorf("name %q must be rooted", r)
		}
	}
	rt.R().MountTable().SetRoots(args)
	return nil, nil, nil, nil
}

type resolve struct {
	mt bool
}

func NewResolve() T {
	return &resolve{}
}

func NewResolveMT() T {
	return &resolve{true}
}

func (*resolve) Help() string {
	return `<name>
	resolve name using the local mountTable client`
}

func (*resolve) Daemon() bool { return false }

func (rs *resolve) Run(args []string) (Variables, []string, Handle, error) {
	if len(args) != 1 {
		return nil, nil, nil, fmt.Errorf("wrong #args: %s", rs.Help())
	}
	name := args[0]
	lmt := rt.R().MountTable()
	var r []string
	v := make(Variables)
	var servers []string
	var err error
	if rs.mt {
		servers, err = lmt.ResolveToMountTable(rt.R().NewContext(), name)
	} else {
		servers, err = lmt.Resolve(rt.R().NewContext(), name)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed resolving %q", name)
	}
	r = append(r, strings.Join(servers, ","))
	for i, s := range servers {
		v.Update("R"+strconv.Itoa(i), s)
	}
	return v, r, nil, nil
}

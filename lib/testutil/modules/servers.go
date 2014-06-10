package modules

import (
	"fmt"
	"os"
	"strings"
	"time"

	"veyron/lib/testutil/blackbox"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
)

func init() {
	blackbox.CommandTable["clock"] = clockChild
	blackbox.CommandTable["echo"] = echoChild
}

func clockChild(args []string) {
	serve("clock", ipc.SoloDispatcher(NewServerClock(&clockServer{}), nil), args)

}

func echoChild(args []string) {
	serve("echo", ipc.SoloDispatcher(NewServerEcho(&echoServer{}), nil), args)
}

func serve(msg string, dispatcher ipc.Dispatcher, args []string) {
	rt.Init()
	if len(args) != 3 {
		bbExitWithError("wrong #args")
	}
	root := args[0]
	prefix := args[1]
	mp := args[2]
	publish := true
	if len(root) == 0 {
		publish = false
	}
	fmt.Println("ready")
	server, err := rt.R().NewServer()
	if err != nil {
		bbExitWithError(fmt.Sprintf("%s failed: %v", msg, err))
	}
	if err := server.Register(prefix, dispatcher); err != nil {
		bbExitWithError(fmt.Sprintf("%s failed: %v", msg, err))
	}
	ep, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		bbExitWithError(fmt.Sprintf("%s failed: %v", msg, err))
	}
	// Sadly, the name a client needs to use to invoke RPCs on us
	// depends on whether we are using a MountTable or not. If we're
	// not using a MountTable then the name is /<ep>/<prefix>, but if we
	// are then it's /<ep of mount table>/<mp>!!
	if publish {
		fmt.Printf("NAME=%s\n", naming.MakeTerminal(naming.JoinAddressName(ep.String(), mp)))
		if err := server.Publish(mp); err != nil {
			bbExitWithError(fmt.Sprintf("%s failed: %v", msg, err))
		}
	} else {
		fmt.Printf("NAME=%s\n", naming.MakeTerminal(naming.JoinAddressName(ep.String(), prefix)))
	}
	fmt.Printf("ADDR=%s\n", ep)
	fmt.Printf("PID=%d\n", os.Getpid())
	fmt.Println("running")
	blackbox.WaitForEOFOnStdin()
}

type clockServer struct{}

func (*clockServer) Time(_ ipc.ServerContext, m string) (string, error) {
	return fmt.Sprintf("(%s) %s", time.Now(), m), nil
}

type echoServer struct{}

func (*echoServer) Echo(_ ipc.ServerContext, m string) (string, error) {
	return m, nil
}

type clock struct{ client bool }

func NewClockServer() T {
	return &clock{false}
}

func NewClockClient() T {
	return &clock{true}
}

func (c *clock) Help() string {
	if c.client {
		return `<name> <message>
executes name.Time(message)`
	}
	return `<root> <prefix> <suffix>
runs a clock server with root as its root mountTable, Register(prefix...) and  Pubish(suffix)`
}

func (c *clock) Daemon() bool { return !c.client }

func (c *clock) Run(args []string) (Variables, []string, Handle, error) {
	if !c.client {
		if len(args) != 3 {
			return nil, nil, nil, fmt.Errorf("wrong #args: %s", c.Help())
		}
		return runServer("clock", args)
	}
	if len(args) != 2 {
		return nil, nil, nil, fmt.Errorf("wrong #args: %s", c.Help())
	}
	name := args[0]
	message := args[1]
	stub, err := BindClock(name)
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := stub.Time(rt.R().NewContext(), message)
	if err != nil {
		return nil, nil, nil, err
	}
	v := make(Variables)
	v.Update("TIME", strings.TrimRight(r, "\n"))
	return v, []string{r}, nil, nil

}

type echo struct{ client bool }

func NewEchoServer() T {
	return &echo{false}
}

func NewEchoClient() T {
	return &echo{true}
}

func (e *echo) Help() string {
	if e.client {
		return `<name> <message>
executes name.Echo(message)`
	}
	return `<root> <prefix> <suffix>
runs a clock server with root as its root mountTable, Register(prefix...) and  Pubish(suffix)`
}

func (e *echo) Daemon() bool { return !e.client }

func (e *echo) Run(args []string) (Variables, []string, Handle, error) {
	if !e.client {
		if len(args) != 3 {
			return nil, nil, nil, fmt.Errorf("wrong #args: %s", e.Help())
		}
		return runServer("echo", args)
	}
	if len(args) != 2 {
		return nil, nil, nil, fmt.Errorf("wrong #args: %s", e.Help())
	}
	name := args[0]
	message := args[1]
	stub, err := BindEcho(name)
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := stub.Echo(rt.R().NewContext(), message)
	if err != nil {
		return nil, nil, nil, err
	}
	v := make(Variables)
	v.Update("ECHO", strings.TrimRight(r, "\n"))
	return v, []string{r}, nil, nil
}

func runServer(name string, args []string) (Variables, []string, Handle, error) {
	env := []string{"MOUNTTABLE_ROOT=" + args[0]}
	c, v, r, err := bbSpawn(name, args, env)
	if err != nil {
		return v, r, nil, err
	}
	h := &handle{c}
	children.add(h)
	return v, r, h, nil
}

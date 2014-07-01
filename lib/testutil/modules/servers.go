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
	if len(args) != 1 {
		bbExitWithError("wrong #args")
	}
	mp := args[0]
	fmt.Println("ready")
	server, err := rt.R().NewServer()
	if err != nil {
		bbExitWithError(fmt.Sprintf("%s failed: %v", msg, err))
	}
	ep, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		bbExitWithError(fmt.Sprintf("%s failed: %v", msg, err))
	}
	fmt.Printf("NAME=%s\n", naming.MakeTerminal(naming.JoinAddressName(ep.String(), "")))
	if err := server.Serve(mp, dispatcher); err != nil {
		bbExitWithError(fmt.Sprintf("%s failed: %v", msg, err))
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
	return `<root> <mountpoint>
runs a clock server with root as its root mount table, mounted at <mountpoint>`
}

func (c *clock) Daemon() bool { return !c.client }

func (c *clock) Run(args []string) (Variables, []string, Handle, error) {
	if !c.client {
		if len(args) != 2 {
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
	return `<root> <mountpoint>
runs a clock server with root as its root mount table, mounted at <mountpoint>`
}

func (e *echo) Daemon() bool { return !e.client }

func (e *echo) Run(args []string) (Variables, []string, Handle, error) {
	if !e.client {
		if len(args) != 2 {
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
	var env []string
	if len(args[0]) > 0 {
		env = append(env, "NAMESPACE_ROOT="+args[0])
	}
	c, v, r, err := bbSpawn(name, args[1:], env)
	if err != nil {
		return v, r, nil, err
	}
	h := &handle{c}
	children.add(h)
	return v, r, h, nil
}

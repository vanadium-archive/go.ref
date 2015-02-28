package core

import (
	"fmt"
	"io"
	"os"
	"time"

	"v.io/v23"
	"v.io/v23/ipc"
	"v.io/v23/security"

	"v.io/core/veyron/lib/modules"
)

func init() {
	modules.RegisterChild(EchoServerCommand, `<name> <message>...
	invokes name.Echo(message)`, echoServer)
	modules.RegisterChild(EchoClientCommand, `<name> <text>
	runs an Echo server mounted at <name> and configured to return <text>: as a prefix in its response`, echoClient)
}

type treeDispatcher struct{ id string }

func (d treeDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	return &echoServerObject{d.id, suffix}, nil, nil
}

type echoServerObject struct {
	id, suffix string
}

func (es *echoServerObject) Echo(call ipc.ServerCall, m string) (string, error) {
	if len(es.suffix) > 0 {
		return fmt.Sprintf("%s.%s: %s\n", es.id, es.suffix, m), nil
	}
	return fmt.Sprintf("%s: %s\n", es.id, m), nil
}

func (es *echoServerObject) Sleep(call ipc.ServerCall, d string) error {
	duration, err := time.ParseDuration(d)
	if err != nil {
		return err
	}
	time.Sleep(duration)
	return nil
}

func echoServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	id, mp := args[0], args[1]
	disp := &treeDispatcher{id: id}
	server, err := v23.NewServer(ctx)
	if err != nil {
		return err
	}
	defer server.Stop()
	eps, err := server.Listen(v23.GetListenSpec(ctx))
	if err != nil {
		return err
	}
	if err := server.ServeDispatcher(mp, disp); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	for _, ep := range eps {
		fmt.Fprintf(stdout, "NAME=%s\n", ep.Name())
	}
	modules.WaitForEOF(stdin)
	return nil
}

func echoClient(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	name := args[0]
	args = args[1:]
	client := v23.GetClient(ctx)
	for _, a := range args {
		h, err := client.StartCall(ctx, name, "Echo", []interface{}{a})
		if err != nil {
			return err
		}
		var r string
		if err := h.Finish(&r); err != nil {
			return err
		}
		fmt.Fprintf(stdout, r)
	}
	return nil
}

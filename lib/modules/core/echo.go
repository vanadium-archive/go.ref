package core

import (
	"fmt"
	"io"
	"os"
	"time"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/security"

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

func (es *echoServerObject) Echo(call ipc.ServerContext, m string) (string, error) {
	if len(es.suffix) > 0 {
		return fmt.Sprintf("%s.%s: %s\n", es.id, es.suffix, m), nil
	}
	return fmt.Sprintf("%s: %s\n", es.id, m), nil
}

func (es *echoServerObject) Sleep(call ipc.ServerContext, d string) error {
	duration, err := time.ParseDuration(d)
	if err != nil {
		return err
	}
	time.Sleep(duration)
	return nil
}

func echoServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	runtime, err := rt.New()
	if err != nil {
		panic(err)
	}
	defer runtime.Cleanup()

	fl, args, err := parseListenFlags(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %s", err)
	}
	if err := checkArgs(args, 2, "<message> <name>"); err != nil {
		return err
	}
	id, mp := args[0], args[1]
	disp := &treeDispatcher{id: id}
	server, err := runtime.NewServer()
	if err != nil {
		return err
	}
	defer server.Stop()
	eps, err := server.Listen(initListenSpec(fl))
	if err != nil {
		return err
	}
	if err := server.ServeDispatcher(mp, disp); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	for _, ep := range eps {
		fmt.Fprintf(stdout, "NAME=%s\n", naming.JoinAddressName(ep.String(), ""))
		fmt.Fprintf(stdout, "ADDR=%s\n", ep)
	}
	modules.WaitForEOF(stdin)
	return nil
}

func echoClient(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	runtime, err := rt.New()
	if err != nil {
		panic(err)
	}
	defer runtime.Cleanup()

	args = args[1:]
	name := args[0]
	args = args[1:]
	client := runtime.Client()
	for _, a := range args {
		ctxt := runtime.NewContext()
		h, err := client.StartCall(ctxt, name, "Echo", []interface{}{a})
		if err != nil {
			return err
		}
		var r string
		var apperr error
		if err := h.Finish(&r, &apperr); err != nil {
			return err
		} else {
			if apperr != nil {
				return apperr
			}
		}
		fmt.Fprintf(stdout, r)
	}
	return nil
}

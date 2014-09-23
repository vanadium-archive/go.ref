package core

import (
	"fmt"
	"io"
	"os"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"

	"veyron.io/veyron/veyron/lib/modules"
)

func init() {
	modules.RegisterChild(EchoServerCommand, echoServer)
	modules.RegisterChild(EchoClientCommand, echoClient)
}

type treeDispatcher struct{ id string }

func (d treeDispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(&echoServerObject{d.id, suffix}), nil, nil
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

func echoServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) != 2 {
		return fmt.Errorf("wrong # args")
	}
	id, mp := args[0], args[1]
	disp := &treeDispatcher{id: id}
	server, err := rt.R().NewServer()
	if err != nil {
		return err
	}
	defer server.Stop()
	ep, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	if err := server.Serve(mp, disp); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "NAME=%s\n", naming.MakeTerminal(naming.JoinAddressName(ep.String(), "")))
	fmt.Fprintf(stdout, "ADDR=%s\n", ep.String())
	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	modules.WaitForEOF(stdin)
	return nil
}

func echoClient(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) < 2 {
		return fmt.Errorf("wrong # args")
	}
	name := args[0]
	args = args[1:]
	client := rt.R().Client()
	for _, a := range args {
		ctxt := rt.R().NewContext()
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

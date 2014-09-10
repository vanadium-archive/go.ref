package core

import (
	"fmt"
	"io"
	"os"
	"strings"

	"veyron2"
	"veyron2/naming"
	"veyron2/rt"

	"veyron/lib/modules"
	mounttable "veyron/services/mounttable/lib"
)

func init() {
	modules.RegisterChild(RootMTCommand, rootMountTable)
	modules.RegisterChild(MTCommand, mountTable)
	modules.RegisterChild(LSExternalCommand, ls)
}

func mountTable(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected exactly one argument: <mount point>")
	}
	return runMT(false, stdin, stdout, stderr, env, args...)
}

func rootMountTable(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) != 0 {
		return fmt.Errorf("expected no arguments")
	}
	return runMT(true, stdin, stdout, stderr, env, args...)
}

func runMT(root bool, stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r := rt.Init()
	server, err := r.NewServer(veyron2.ServesMountTableOpt(true))
	if err != nil {
		return fmt.Errorf("root failed: %v", err)
	}
	mp := ""
	if !root {
		mp = args[0]
	}
	mt, err := mounttable.NewMountTable("")
	if err != nil {
		return fmt.Errorf("mounttable.NewMountTable failed: %s", err)
	}
	ep, err := server.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("server.Listen failed: %s", err)
	}
	if err := server.Serve(mp, mt); err != nil {
		return fmt.Errorf("root failed: %s", err)
	}
	name := naming.JoinAddressName(ep.String(), "")
	fmt.Fprintf(os.Stderr, "Serving MountTable on %q", name)

	fmt.Printf("MT_NAME=%s\n", name)
	fmt.Printf("PID=%d\n", os.Getpid())
	modules.WaitForEOF(stdin)
	fmt.Println("done\n")
	return nil
}

func ls(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	details := false
	if len(args) > 0 && args[0] == "-l" {
		details = true
		args = args[1:]
	}

	ns := rt.R().Namespace()
	for _, pattern := range args {
		ch, err := ns.Glob(rt.R().NewContext(), pattern)
		if err != nil {
			return err
		}
		for n := range ch {
			if details {
				fmt.Fprintf(stdout, "%s [", n.Name)
				t := ""
				for _, s := range n.Servers {
					t += fmt.Sprintf("%s:%ss, ", s.Server, s.TTL)
				}
				t = strings.TrimSuffix(t, ", ")
				fmt.Fprintf(stdout, "%s]\n", t)
			} else {
				if len(n.Name) > 0 {
					fmt.Fprintf(stdout, "%s\n", n.Name)
				}
			}
		}
	}
	return nil
}

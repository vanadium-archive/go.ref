package core

import (
	"fmt"
	"io"
	"os"
	"strings"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/modules"
	mounttable "veyron.io/veyron/veyron/services/mounttable/lib"
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
	fmt.Fprintf(stdout, "MT_NAME=%s\n", name)
	fmt.Fprintf(stdout, "MT_ADDR=%s\n", ep.String())
	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	modules.WaitForEOF(stdin)
	return nil
}

func ls(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	details := false
	if len(args) > 0 && args[0] == "-l" {
		details = true
		args = args[1:]
	}
	ns := rt.R().Namespace()
	entry := 0
	output := ""
	for _, pattern := range args {
		ch, err := ns.Glob(rt.R().NewContext(), pattern)
		if err != nil {
			return err
		}
		for n := range ch {
			if details {
				output += fmt.Sprintf("R%d=%s[", entry, n.Name)
				t := ""
				for _, s := range n.Servers {
					t += fmt.Sprintf("%s:%s, ", s.Server, s.TTL)
				}
				t = strings.TrimSuffix(t, ", ")
				output += fmt.Sprintf("%s]\n", t)
				entry += 1
			} else {
				if len(n.Name) > 0 {
					output += fmt.Sprintf("R%d=%s\n", entry, n.Name)
					entry += 1
				}
			}

		}
	}
	fmt.Fprintf(stdout, "RN=%d\n", entry)
	fmt.Fprint(stdout, output)
	return nil
}

type resolver func(ctx context.T, name string) (names []string, err error)

func resolve(fn resolver, stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) != 1 {
		return fmt.Errorf("wrong # args")
	}
	name := args[0]
	servers, err := fn(rt.R().NewContext(), name)
	if err != nil {
		fmt.Fprintf(stdout, "RN=0\n")
		return err
	}
	fmt.Fprintf(stdout, "RN=%d\n", len(servers))
	for i, s := range servers {
		fmt.Fprintf(stdout, "R%d=%s\n", i, s)
	}
	return nil
}

func resolveObject(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return resolve(rt.R().Namespace().Resolve, stdin, stdout, stderr, env, args...)
}

func resolveMT(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return resolve(rt.R().Namespace().ResolveToMountTable, stdin, stdout, stderr, env, args...)
}

func setNamespaceRoots(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ns := rt.R().Namespace()
	return ns.SetRoots(args...)
}

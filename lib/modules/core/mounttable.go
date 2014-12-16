package core

import (
	"fmt"
	"io"
	"os"
	"strings"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/modules"
	mounttable "veyron.io/veyron/veyron/services/mounttable/lib"
)

func init() {
	modules.RegisterChild(RootMTCommand, "", rootMountTable)
	modules.RegisterChild(MTCommand, `<mount point>
	reads NAMESPACE_ROOT from its environment and mounts a new mount table at <mount point>`, mountTable)
	modules.RegisterChild(LSCommand, `<glob>...
	issues glob requests using the current processes namespace library`,
		ls)
}

func mountTable(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return runMT(false, stdin, stdout, stderr, env, args...)
}

func rootMountTable(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return runMT(true, stdin, stdout, stderr, env, args...)
}

func runMT(root bool, stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r, err := rt.New()
	if err != nil {
		panic(err)
	}
	defer r.Cleanup()

	fl, args, err := parseListenFlags(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %s", err)
	}
	lspec := initListenSpec(fl)
	server, err := r.NewServer(options.ServesMountTable(true))
	if err != nil {
		return fmt.Errorf("root failed: %v", err)
	}
	mp := ""
	if !root {
		if err := checkArgs(args, 1, "<mount point>"); err != nil {
			return err
		}
		mp = args[0]
	}
	mt, err := mounttable.NewMountTable("")
	if err != nil {
		return fmt.Errorf("mounttable.NewMountTable failed: %s", err)
	}
	eps, err := server.Listen(lspec)
	if err != nil {
		return fmt.Errorf("server.Listen failed: %s", err)
	}
	if err := server.ServeDispatcher(mp, mt); err != nil {
		return fmt.Errorf("root failed: %s", err)
	}
	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	for _, ep := range eps {
		name := naming.JoinAddressName(ep.String(), "")
		fmt.Fprintf(stdout, "MT_NAME=%s\n", name)
		fmt.Fprintf(stdout, "MT_ADDR=%s\n", ep.String())
	}
	modules.WaitForEOF(stdin)
	return nil
}

func ls(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r, err := rt.New()
	if err != nil {
		panic(err)
	}
	defer r.Cleanup()

	details := false
	args = args[1:] // skip over command name
	if len(args) > 0 && args[0] == "-l" {
		details = true
		args = args[1:]
	}
	ns := r.Namespace()
	entry := 0
	output := ""
	for _, pattern := range args {
		ch, err := ns.Glob(r.NewContext(), pattern)
		if err != nil {
			return err
		}
		for n := range ch {
			if details {
				output += fmt.Sprintf("R%d=%s[", entry, n.Name)
				t := ""
				for _, s := range n.Servers {
					t += fmt.Sprintf("%s:%s, ", s.Server, s.Expires)
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

package main

import (
	"fmt"
	"io"
	"time"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/naming"

	"veyron.io/veyron/veyron/lib/modules"
)

func init() {
	modules.RegisterFunction("cache", `on|off
turns the namespace cache on or off`, namespaceCache)
	modules.RegisterFunction("mount", `<mountpoint> <server> <ttl> [M][R]
invokes namespace.Mount(<mountpoint>, <server>, <ttl>)`, mountServer)
	modules.RegisterFunction("resolve", `<name>
resolves name to obtain an object server address`, resolveObject)
	modules.RegisterFunction("resolveMT", `<name>
resolves name to obtain a mount table address`, resolveMT)
	modules.RegisterFunction("setRoots", `<name>...
set the in-process namespace roots to <name>...`, setNamespaceRoots)
}

// checkArgs checks for the expected number of args in args. A negative
// value means at least that number of args are expected.
func checkArgs(args []string, expected int, usage string) error {
	got := len(args)
	if expected < 0 {
		expected = -expected
		if got < expected {
			return fmt.Errorf("wrong # args (got %d, expected >=%d) expected: %q got: %v", got, expected, usage, args)
		}
	} else {
		if got != expected {
			return fmt.Errorf("wrong # args (got %d, expected %d) expected: %q got: %v", got, expected, usage, args)
		}
	}
	return nil
}

func mountServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if err := checkArgs(args[1:], -3, "<mount point> <server> <ttl> [M][R]"); err != nil {
		return err
	}
	var opts []naming.MountOpt
	for _, arg := range args[4:] {
		for _, c := range arg {
			switch c {
			case 'R':
				opts = append(opts, naming.ReplaceMountOpt(true))
			case 'M':
				opts = append(opts, naming.ServesMountTableOpt(true))
			}
		}
	}
	mp, server, ttlstr := args[1], args[2], args[3]
	ttl, err := time.ParseDuration(ttlstr)
	if err != nil {
		return fmt.Errorf("failed to parse time from %q", ttlstr)
	}
	ns := runtime.Namespace()
	if err := ns.Mount(runtime.NewContext(), mp, server, ttl, opts...); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Mount(%s, %s, %s, %v)\n", mp, server, ttl, opts)
	return nil
}

func namespaceCache(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if err := checkArgs(args[1:], 1, "on|off"); err != nil {
		return err
	}
	disable := true
	switch args[1] {
	case "on":
		disable = false
	case "off":
		disable = true
	default:
		return fmt.Errorf("arg must be 'on' or 'off'")
	}
	runtime.Namespace().CacheCtl(naming.DisableCache(disable))
	return nil
}

type resolver func(ctx context.T, name string, opts ...naming.ResolveOpt) (names []string, err error)

func resolve(fn resolver, stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if err := checkArgs(args[1:], 1, "<name>"); err != nil {
		return err
	}
	name := args[1]
	servers, err := fn(runtime.NewContext(), name)
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
	return resolve(runtime.Namespace().Resolve, stdin, stdout, stderr, env, args...)
}

func resolveMT(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return resolve(runtime.Namespace().ResolveToMountTable, stdin, stdout, stderr, env, args...)
}

func setNamespaceRoots(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return runtime.Namespace().SetRoots(args[1:]...)
}

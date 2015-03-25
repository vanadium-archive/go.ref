package main

import (
	"fmt"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/x/lib/cmdline"
	"v.io/x/lib/vlog"
)

var (
	flagInsecureResolve     bool
	flagInsecureResolveToMT bool
)

var cmdGlob = &cmdline.Command{
	Run:      runGlob,
	Name:     "glob",
	Short:    "Returns all matching entries from the namespace",
	Long:     "Returns all matching entries from the namespace.",
	ArgsName: "<pattern>",
	ArgsLong: `
<pattern> is a glob pattern that is matched against all the names below the
specified mount name.
`,
}

func runGlob(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("glob: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	pattern := args[0]

	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()

	ns := v23.GetNamespace(ctx)

	c, err := ns.Glob(ctx, pattern)
	if err != nil {
		vlog.Infof("ns.Glob(%q) failed: %v", pattern, err)
		return err
	}
	for res := range c {
		switch v := res.(type) {
		case *naming.MountEntry:
			fmt.Fprint(cmd.Stdout(), v.Name)
			for _, s := range v.Servers {
				fmt.Fprintf(cmd.Stdout(), " %s (Deadline %s)", s.Server, s.Deadline.Time)
			}
			fmt.Fprintln(cmd.Stdout())
		case *naming.GlobError:
			fmt.Fprintf(cmd.Stderr(), "Error: %s: %v\n", v.Name, v.Error)
		}
	}
	return nil
}

var cmdMount = &cmdline.Command{
	Run:      runMount,
	Name:     "mount",
	Short:    "Adds a server to the namespace",
	Long:     "Adds server <server> to the namespace with name <name>.",
	ArgsName: "<name> <server> <ttl>",
	ArgsLong: `
<name> is the name to add to the namespace.
<server> is the object address of the server to add.
<ttl> is the TTL of the new entry. It is a decimal number followed by a unit
suffix (s, m, h). A value of 0s represents an infinite duration.
`,
}

func runMount(cmd *cmdline.Command, args []string) error {
	if expected, got := 3, len(args); expected != got {
		return cmd.UsageErrorf("mount: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	name := args[0]
	server := args[1]
	ttlArg := args[2]

	ttl, err := time.ParseDuration(ttlArg)
	if err != nil {
		return fmt.Errorf("TTL parse error: %v", err)
	}

	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()

	ns := v23.GetNamespace(ctx)
	if err = ns.Mount(ctx, name, server, ttl); err != nil {
		vlog.Infof("ns.Mount(%q, %q, %s) failed: %v", name, server, ttl, err)
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Server mounted successfully.")
	return nil
}

var cmdUnmount = &cmdline.Command{
	Run:      runUnmount,
	Name:     "unmount",
	Short:    "Removes a server from the namespace",
	Long:     "Removes server <server> with name <name> from the namespace.",
	ArgsName: "<name> <server>",
	ArgsLong: `
<name> is the name to remove from the namespace.
<server> is the object address of the server to remove.
`,
}

func runUnmount(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("unmount: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	name := args[0]
	server := args[1]

	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()

	ns := v23.GetNamespace(ctx)

	if err := ns.Unmount(ctx, name, server); err != nil {
		vlog.Infof("ns.Unmount(%q, %q) failed: %v", name, server, err)
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Server unmounted successfully.")
	return nil
}

var cmdResolve = &cmdline.Command{
	Run:      runResolve,
	Name:     "resolve",
	Short:    "Translates a object name to its object address(es)",
	Long:     "Translates a object name to its object address(es).",
	ArgsName: "<name>",
	ArgsLong: "<name> is the name to resolve.",
}

func runResolve(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("resolve: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()

	ns := v23.GetNamespace(ctx)

	var opts []naming.ResolveOpt
	if flagInsecureResolve {
		opts = append(opts, options.SkipServerEndpointAuthorization{})
	}
	me, err := ns.Resolve(ctx, name, opts...)
	if err != nil {
		vlog.Infof("ns.Resolve(%q) failed: %v", name, err)
		return err
	}
	for _, n := range me.Names() {
		fmt.Fprintln(cmd.Stdout(), n)
	}
	return nil
}

var cmdResolveToMT = &cmdline.Command{
	Run:      runResolveToMT,
	Name:     "resolvetomt",
	Short:    "Finds the address of the mounttable that holds an object name",
	Long:     "Finds the address of the mounttable that holds an object name.",
	ArgsName: "<name>",
	ArgsLong: "<name> is the name to resolve.",
}

func runResolveToMT(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("resolvetomt: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()

	ns := v23.GetNamespace(ctx)
	var opts []naming.ResolveOpt
	if flagInsecureResolveToMT {
		opts = append(opts, options.SkipServerEndpointAuthorization{})
	}
	e, err := ns.ResolveToMountTable(ctx, name, opts...)
	if err != nil {
		vlog.Infof("ns.ResolveToMountTable(%q) failed: %v", name, err)
		return err
	}
	for _, s := range e.Servers {
		fmt.Fprintln(cmd.Stdout(), naming.JoinAddressName(s.Server, e.Name))
	}
	return nil
}

func root() *cmdline.Command {
	cmdResolve.Flags.BoolVar(&flagInsecureResolve, "insecure", false, "Insecure mode: May return results from untrusted servers and invoke Resolve on untrusted mounttables")
	cmdResolveToMT.Flags.BoolVar(&flagInsecureResolveToMT, "insecure", false, "Insecure mode: May return results from untrusted servers and invoke Resolve on untrusted mounttables")
	return &cmdline.Command{
		Name:  "namespace",
		Short: "Tool for interacting with the Vanadium namespace",
		Long: `
The namespace tool facilitates interaction with the Vanadium namespace.

The namespace roots are set from the command line via veyron.namespace.root options or from environment variables that have a name
starting with NAMESPACE_ROOT, e.g. NAMESPACE_ROOT, NAMESPACE_ROOT_2,
NAMESPACE_ROOT_GOOGLE, etc. The command line options override the environment.
`,
		Children: []*cmdline.Command{cmdGlob, cmdMount, cmdUnmount, cmdResolve, cmdResolveToMT},
	}
}

package main

import (
	"fmt"
	"time"

	"v.io/lib/cmdline"
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/vlog"
)

var (
	flagInsecureResolve     bool
	flagInsecureResolveToMT bool
	flagMountBlessings      listFlag
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
				fmt.Fprintf(cmd.Stdout(), " %v%s (Expires %s)", fmtBlessingPatterns(s.BlessingPatterns), s.Server, s.Expires)
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

	blessings := flagMountBlessings.list
	if len(blessings) == 0 {
		// Obtain the blessings of the running server so it can be mounted with
		// those blessings.
		if blessings, err = blessingsOfRunningServer(ctx, server); err != nil {
			return err
		}
		vlog.Infof("Server at %q has blessings %v", server, blessings)
	}
	ns := v23.GetNamespace(ctx)
	if err = ns.Mount(ctx, name, server, ttl, naming.MountedServerBlessingsOpt(blessings)); err != nil {
		vlog.Infof("ns.Mount(%q, %q, %s, %v) failed: %v", name, server, ttl, blessings, err)
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
		opts = append(opts, options.SkipResolveAuthorization{})
	}
	me, err := ns.Resolve(ctx, name, opts...)
	if err != nil {
		vlog.Infof("ns.Resolve(%q) failed: %v", name, err)
		return err
	}
	for i := range me.Servers {
		fmt.Fprintln(cmd.Stdout(), fmtServer(me, i))
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
		opts = append(opts, options.SkipResolveAuthorization{})
	}
	e, err := ns.ResolveToMountTable(ctx, name, opts...)
	if err != nil {
		vlog.Infof("ns.ResolveToMountTable(%q) failed: %v", name, err)
		return err
	}
	for i := range e.Servers {
		fmt.Fprintln(cmd.Stdout(), fmtServer(e, i))
	}
	return nil
}

func root() *cmdline.Command {
	cmdResolve.Flags.BoolVar(&flagInsecureResolve, "insecure", false, "Insecure mode: May return results from untrusted servers and invoke Resolve on untrusted mounttables")
	cmdResolveToMT.Flags.BoolVar(&flagInsecureResolveToMT, "insecure", false, "Insecure mode: May return results from untrusted servers and invoke Resolve on untrusted mounttables")
	cmdMount.Flags.Var(&flagMountBlessings, "blessing_pattern", "Blessing pattern that is matched by the blessings of the server being mounted. If none is provided, the server will be contacted to determine this value.")
	return &cmdline.Command{
		Name:  "namespace",
		Short: "Tool for interacting with the Veyron namespace",
		Long: `
The namespace tool facilitates interaction with the Veyron namespace.

The namespace roots are set from the command line via veyron.namespace.root options or from environment variables that have a name
starting with NAMESPACE_ROOT, e.g. NAMESPACE_ROOT, NAMESPACE_ROOT_2,
NAMESPACE_ROOT_GOOGLE, etc. The command line options override the environment.
`,
		Children: []*cmdline.Command{cmdGlob, cmdMount, cmdUnmount, cmdResolve, cmdResolveToMT},
	}
}

func fmtServer(m *naming.MountEntry, idx int) string {
	s := m.Servers[idx]
	return fmt.Sprintf("%v%s", fmtBlessingPatterns(s.BlessingPatterns), naming.JoinAddressName(s.Server, m.Name))
}

func fmtBlessingPatterns(p []string) string {
	if len(p) == 0 {
		return ""
	}
	return fmt.Sprintf("%v", p)
}

func blessingsOfRunningServer(ctx *context.T, server string) ([]string, error) {
	vlog.Infof("Contacting %q to determine the blessings presented by it", server)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	call, err := v23.GetClient(ctx).StartCall(ctx, server, ipc.ReservedSignature, nil)
	if err != nil {
		return nil, fmt.Errorf("Unable to extract blessings presented by %q: %v", server, err)
	}
	blessings, _ := call.RemoteBlessings()
	if len(blessings) == 0 {
		return nil, fmt.Errorf("No recognizable blessings presented by %q, it cannot be securely mounted", server)
	}
	return blessings, nil
}

type listFlag struct {
	list []string
}

func (l *listFlag) Set(s string) error {
	l.list = append(l.list, s)
	return nil
}

func (l *listFlag) Get() interface{} { return l.list }

func (l *listFlag) String() string { return fmt.Sprintf("%v", l.list) }

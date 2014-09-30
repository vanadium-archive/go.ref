package impl

import (
	"fmt"
	"time"

	"veyron.io/veyron/veyron/lib/cmdline"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/services/mounttable"
)

func bindMT(ctx context.T, name string) (mounttable.MountTable, error) {
	mts, err := rt.R().Namespace().ResolveToMountTable(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(mts) == 0 {
		return nil, fmt.Errorf("Failed to find any mount tables at %q", name)
	}
	fmt.Println(mts)
	return mounttable.BindMountTable(mts[0])
}

var cmdGlob = &cmdline.Command{
	Run:      runGlob,
	Name:     "glob",
	Short:    "returns all matching entries in the mount table",
	Long:     "returns all matching entries in the mount table",
	ArgsName: "<mount name> <pattern>",
	ArgsLong: `
<mount name> is a mount name on a mount table.
<pattern> is a glob pattern that is matched against all the entries below the
specified mount name.
`,
}

func runGlob(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("glob: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	c, err := bindMT(ctx, args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	stream, err := c.Glob(ctx, args[1])
	if err != nil {
		return err
	}
	rStream := stream.RecvStream()
	for rStream.Advance() {
		buf := rStream.Value()

		fmt.Fprint(cmd.Stdout(), buf.Name)
		for _, s := range buf.Servers {
			fmt.Fprintf(cmd.Stdout(), " %s (TTL %s)", s.Server, time.Duration(s.TTL)*time.Second)
		}
		fmt.Fprintln(cmd.Stdout())
	}

	if err := rStream.Err(); err != nil {
		return fmt.Errorf("advance error: %v", err)
	}
	err = stream.Finish()
	if err != nil {
		return fmt.Errorf("finish error: %v", err)
	}
	return nil
}

var cmdMount = &cmdline.Command{
	Run:      runMount,
	Name:     "mount",
	Short:    "Mounts a server <name> onto a mount table",
	Long:     "Mounts a server <name> onto a mount table",
	ArgsName: "<mount name> <name> <ttl>",
	ArgsLong: `
<mount name> is a mount name on a mount table.
<name> is the rooted object name of the server.
<ttl> is the TTL of the new entry. It is a decimal number followed by a unit
suffix (s, m, h). A value of 0s represents an infinite duration.
`,
}

func runMount(cmd *cmdline.Command, args []string) error {
	if expected, got := 3, len(args); expected != got {
		return cmd.UsageErrorf("mount: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	c, err := bindMT(ctx, args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	ttl, err := time.ParseDuration(args[2])
	if err != nil {
		return fmt.Errorf("TTL parse error: %v", err)
	}
	err = c.Mount(ctx, args[1], uint32(ttl.Seconds()))
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.Stdout(), "Name mounted successfully.")
	return nil
}

var cmdUnmount = &cmdline.Command{
	Run:      runUnmount,
	Name:     "unmount",
	Short:    "removes server <name> from the mount table",
	Long:     "removes server <name> from the mount table",
	ArgsName: "<mount name> <name>",
	ArgsLong: `
<mount name> is a mount name on a mount table.
<name> is the rooted object name of the server.
`,
}

func runUnmount(cmd *cmdline.Command, args []string) error {
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("unmount: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	c, err := bindMT(ctx, args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	err = c.Unmount(ctx, args[1])
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.Stdout(), "Name unmounted successfully.")
	return nil
}

var cmdResolveStep = &cmdline.Command{
	Run:      runResolveStep,
	Name:     "resolvestep",
	Short:    "takes the next step in resolving a name.",
	Long:     "takes the next step in resolving a name.",
	ArgsName: "<mount name>",
	ArgsLong: `
<mount name> is a mount name on a mount table.
`,
}

func runResolveStep(cmd *cmdline.Command, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return cmd.UsageErrorf("mount: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	ctx, cancel := rt.R().NewContext().WithTimeout(time.Minute)
	defer cancel()
	c, err := bindMT(ctx, args[0])
	if err != nil {
		return fmt.Errorf("bind error: %v", err)
	}
	servers, suffix, err := c.ResolveStep(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.Stdout(), "Servers: %v Suffix: %q\n", servers, suffix)
	return nil
}

func Root() *cmdline.Command {
	return &cmdline.Command{
		Name:     "mounttable",
		Short:    "Command-line tool for interacting with a Veyron mount table",
		Long:     "Command-line tool for interacting with a Veyron mount table",
		Children: []*cmdline.Command{cmdGlob, cmdMount, cmdUnmount, cmdResolveStep},
	}
}

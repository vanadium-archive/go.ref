package main

import (
	"errors"
	"fmt"
	"time"

	"veyron.io/lib/cmdline"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
)

var cmdGlob = &cmdline.Command{
	Run:      runGlob,
	Name:     "glob",
	Short:    "returns all matching entries in the mount table",
	Long:     "returns all matching entries in the mount table",
	ArgsName: "[<mount name>] <pattern>",
	ArgsLong: `
<mount name> is a mount name on a mount table.  Defaults to namespace root.
<pattern> is a glob pattern that is matched against all the entries below the
specified mount name.
`,
}

func runGlob(cmd *cmdline.Command, args []string) error {
	if len(args) == 1 {
		roots := runtime.Namespace().Roots()
		if len(roots) == 0 {
			return errors.New("no namespace root")
		}
		args = append([]string{roots[0]}, args...)
	}
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("glob: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
	defer cancel()
	name, pattern := args[0], args[1]
	call, err := runtime.Client().StartCall(ctx, name, ipc.GlobMethod, []interface{}{pattern}, options.NoResolve(true))
	if err != nil {
		return err
	}
	for {
		var me naming.VDLMountEntry
		if err := call.Recv(&me); err != nil {
			break
		}
		fmt.Fprint(cmd.Stdout(), me.Name)
		for _, s := range me.Servers {
			fmt.Fprintf(cmd.Stdout(), " %s (TTL %s)", s.Server, time.Duration(s.TTL)*time.Second)
		}
		fmt.Fprintln(cmd.Stdout())
	}
	if ferr := call.Finish(&err); ferr != nil {
		err = ferr
	}
	return err
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
	got := len(args)
	if got < 2 || got > 4 {
		return cmd.UsageErrorf("mount: incorrect number of arguments, expected 2, 3, or 4, got %d", got)
	}
	var flags naming.MountFlag
	var seconds uint32
	if got >= 3 {
		ttl, err := time.ParseDuration(args[2])
		if err != nil {
			return fmt.Errorf("TTL parse error: %v", err)
		}
		seconds = uint32(ttl.Seconds())
	}
	if got >= 4 {
		for _, c := range args[3] {
			switch c {
			case 'M':
				flags |= naming.MountFlag(naming.MT)
			case 'R':
				flags |= naming.MountFlag(naming.Replace)
			}
		}
	}
	ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
	defer cancel()
	call, err := runtime.Client().StartCall(ctx, args[0], "Mount", []interface{}{args[1], seconds, 0}, options.NoResolve(true))
	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
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
	ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
	defer cancel()
	call, err := runtime.Client().StartCall(ctx, args[0], "Unmount", []interface{}{args[1]}, options.NoResolve(true))
	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.Stdout(), "Name unmounted successfully.")
	return err
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
	ctx, cancel := runtime.NewContext().WithTimeout(time.Minute)
	defer cancel()
	call, err := runtime.Client().StartCall(ctx, args[0], "ResolveStepX", []interface{}{}, options.NoResolve(true))
	if err != nil {
		return err
	}
	var entry naming.VDLMountEntry
	if ierr := call.Finish(&entry, &err); ierr != nil {
		err = ierr
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.Stdout(), "Servers: %v Suffix: %q MT: %v\n", entry.Servers, entry.Name, entry.MT)
	return nil
}

func root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "mounttable",
		Short: "Tool for interacting with a Veyron mount table",
		Long: `
The mounttable tool facilitates interaction with a Veyron mount table.
`,
		Children: []*cmdline.Command{cmdGlob, cmdMount, cmdUnmount, cmdResolveStep},
	}
}

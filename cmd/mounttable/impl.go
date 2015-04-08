// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"

	"v.io/x/lib/cmdline"
	"v.io/x/lib/vlog"
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
	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()

	if len(args) == 1 {
		roots := v23.GetNamespace(ctx).Roots()
		if len(roots) == 0 {
			return errors.New("no namespace root")
		}
		args = append([]string{roots[0]}, args...)
	}
	if expected, got := 2, len(args); expected != got {
		return cmd.UsageErrorf("glob: incorrect number of arguments, expected %d, got %d", expected, got)
	}

	name, pattern := args[0], args[1]
	client := v23.GetClient(ctx)
	call, err := client.StartCall(ctx, name, rpc.GlobMethod, []interface{}{pattern}, options.NoResolve{})
	if err != nil {
		return err
	}
	for {
		var gr naming.GlobReply
		if err := call.Recv(&gr); err != nil {
			break
		}
		switch v := gr.(type) {
		case naming.GlobReplyEntry:
			fmt.Fprint(cmd.Stdout(), v.Value.Name)
			for _, s := range v.Value.Servers {
				fmt.Fprintf(cmd.Stdout(), " %s (Deadline %s)", s.Server, s.Deadline.Time)
			}
			fmt.Fprintln(cmd.Stdout())
		}
	}
	if err := call.Finish(); err != nil {
		return err
	}
	return nil
}

var cmdMount = &cmdline.Command{
	Run:      runMount,
	Name:     "mount",
	Short:    "Mounts a server <name> onto a mount table",
	Long:     "Mounts a server <name> onto a mount table",
	ArgsName: "<mount name> <name> <ttl> [M|R]",
	ArgsLong: `
<mount name> is a mount name on a mount table.

<name> is the rooted object name of the server.

<ttl> is the TTL of the new entry. It is a decimal number followed by a unit
suffix (s, m, h). A value of 0s represents an infinite duration.

[M|R] are mount options. M indicates that <name> is a mounttable. R indicates
that existing entries should be removed.
`,
}

func runMount(cmd *cmdline.Command, args []string) error {
	got := len(args)
	if got < 2 || got > 4 {
		return cmd.UsageErrorf("mount: incorrect number of arguments, expected 2, 3, or 4, got %d", got)
	}
	name := args[0]
	server := args[1]

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
	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()
	client := v23.GetClient(ctx)
	call, err := client.StartCall(ctx, name, "Mount", []interface{}{server, seconds, flags}, options.NoResolve{})
	if err != nil {
		return err
	}
	if err := call.Finish(); err != nil {
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
	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()
	client := v23.GetClient(ctx)
	call, err := client.StartCall(ctx, args[0], "Unmount", []interface{}{args[1]}, options.NoResolve{})
	if err != nil {
		return err
	}
	if err := call.Finish(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Stdout(), "Unmount successful or name not mounted.")
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
	ctx, cancel := context.WithTimeout(gctx, time.Minute)
	defer cancel()
	client := v23.GetClient(ctx)
	call, err := client.StartCall(ctx, args[0], "ResolveStep", []interface{}{}, options.NoResolve{})
	if err != nil {
		return err
	}
	var entry naming.MountEntry
	if err := call.Finish(&entry); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Servers: %v Suffix: %q MT: %v\n", entry.Servers, entry.Name, entry.ServesMountTable)
	return nil
}

func root() *cmdline.Command {
	return &cmdline.Command{
		Name:  "mounttable",
		Short: "sends commands to Vanadium mounttable services",
		Long: `
Command mounttable sends commands to Vanadium mounttable services.
`,
		Children: []*cmdline.Command{cmdGlob, cmdMount, cmdUnmount, cmdResolveStep},
	}
}

func blessingPatternsFromServer(ctx *context.T, server string) ([]security.BlessingPattern, error) {
	vlog.Infof("Contacting %q to determine the blessings presented by it", server)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	call, err := v23.GetClient(ctx).StartCall(ctx, server, rpc.ReservedSignature, nil)
	if err != nil {
		return nil, fmt.Errorf("Unable to extract blessings presented by %q: %v", server, err)
	}
	blessings, _ := call.RemoteBlessings()
	if len(blessings) == 0 {
		return nil, fmt.Errorf("No recognizable blessings presented by %q, it cannot be securely mounted", server)
	}
	// This translation between BlessingPattern and string is silly!
	// Kill the BlessingPatterns type and make methods on that type
	// functions instead!
	patterns := make([]security.BlessingPattern, len(blessings))
	for i := range blessings {
		patterns[i] = security.BlessingPattern(blessings[i])
	}
	return patterns, nil
}

// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"fmt"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/allocator"
)

var (
	flagAllocator string

	cmdRoot = &cmdline.Command{
		Name:  "allocator",
		Short: "Command allocator interacts with the allocator service.",
		Long:  "Command allocator interacts with the allocator service.",
		Children: []*cmdline.Command{
			cmdCreate,
			cmdDelete,
			cmdList,
		},
	}
)

func main() {
	cmdRoot.Flags.StringVar(&flagAllocator, "allocator", "", "The name or address of the allocator server.")
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

var cmdCreate = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(runCreate),
	Name:     "create",
	Short:    "Create a new server instance.",
	Long:     "Create a new server instance.",
	ArgsName: "<extension>",
	ArgsLong: `
<extension> is the blessing name extension to give to the new server instance.
`,
}

func runCreate(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 1, len(args); got != expected {
		return env.UsageErrorf("create: incorrect number of arguments, got %d, expected %d", got, expected)
	}
	extension := args[0]

	name, err := allocator.AllocatorClient(flagAllocator).Create(ctx, &granter{extension: extension})
	if err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, name)
	return nil
}

type granter struct {
	rpc.CallOpt
	extension string
}

func (g *granter) Grant(ctx *context.T, call security.Call) (security.Blessings, error) {
	p := call.LocalPrincipal()
	def, _ := p.BlessingStore().Default()
	return p.Bless(call.RemoteBlessings().PublicKey(), def, g.extension, security.UnconstrainedUse())
}

var cmdDelete = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(runDelete),
	Name:     "delete",
	Short:    "Deletes an existing server instance.",
	Long:     "Deletes an existing server instance.",
	ArgsName: "<name>",
	ArgsLong: `
<name> is the name of the server to delete.
`,
}

func runDelete(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 1, len(args); got != expected {
		return env.UsageErrorf("delete: incorrect number of arguments, got %d, expected %d", got, expected)
	}
	name := args[0]
	if err := allocator.AllocatorClient(flagAllocator).Delete(ctx, name); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Deleted")
	return nil
}

var cmdList = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runList),
	Name:   "list",
	Short:  "Lists existing server instances.",
	Long:   "Lists existing server instances.",
}

func runList(ctx *context.T, env *cmdline.Env, args []string) error {
	names, err := allocator.AllocatorClient(flagAllocator).List(ctx)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(env.Stderr, "No results")
	}
	for _, n := range names {
		fmt.Fprintln(env.Stdout, n)
	}
	return nil
}

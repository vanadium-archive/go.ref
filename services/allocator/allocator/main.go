// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"fmt"

	"v.io/v23/context"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/allocator"
)

const defaultExtension = "allocator"

var (
	flagAllocator string

	cmdRoot = &cmdline.Command{
		Name:  "allocator",
		Short: "Command allocator interacts with the allocator service.",
		Long:  "Command allocator interacts with the allocator service.",
		Children: []*cmdline.Command{
			cmdCreate,
			cmdDestroy,
			cmdList,
		},
	}
)

func main() {
	cmdRoot.Flags.StringVar(&flagAllocator, "allocator", "syncbase-allocator", "The name or address of the allocator server.")
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

var cmdCreate = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runCreate),
	Name:   "create",
	Short:  "Create a new server instance.",
	Long:   "Create a new server instance.",
}

func runCreate(ctx *context.T, env *cmdline.Env, args []string) error {
	name, err := allocator.AllocatorClient(flagAllocator).Create(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, name)
	return nil
}

var cmdDestroy = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(runDestroy),
	Name:     "destroy",
	Short:    "Destroys an existing server instance.",
	Long:     "Destroys an existing server instance.",
	ArgsName: "<name>",
	ArgsLong: `
<name> is the name of the server to destroy.
`,
}

func runDestroy(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 1, len(args); got != expected {
		return env.UsageErrorf("destroy: incorrect number of arguments, got %d, expected %d", got, expected)
	}
	name := args[0]
	if err := allocator.AllocatorClient(flagAllocator).Destroy(ctx, name); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Destroyed")
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

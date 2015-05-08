// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package v23cmd implements utilities for running v23 cmdline programs.
//
// The cmdline package requires a Runner that implements Run(env, args), but for
// v23 programs we'd like to implement Run(ctx, env, args) instead.  The
// initialization of the ctx is also tricky, since v23.Init() calls flag.Parse,
// but the cmdline package needs to call flag.Parse first.
//
// The RunnerFunc package-level function allows us to write run functions of the
// form Run(ctx, env, args), retaining static type-safety, and also getting the
// flag.Parse ordering right.  In addition the Run and ParseAndRun functions may
// be called in tests, to pass your own ctx into your command runners.
package v23cmd

import (
	"errors"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/x/lib/cmdline2"
)

var (
	ErrUnknownRunner = errors.New("v23cmd: unknown runner")

	// The strategy behind this package is this slice, which only grows and never
	// shrinks.  Here we maintain a mapping between an index number and the
	// originally registered function, so that we can look the function up in a
	// typesafe manner.
	funcs  []func(*context.T, *cmdline2.Env, []string) error
	initFn = v23.Init
)

// indexRunner implements cmdline.Runner by looking up the index in the funcs
// slice to retrieve the originally registered function.
type indexRunner uint

func (ix indexRunner) Run(env *cmdline2.Env, args []string) error {
	if int(ix) < len(funcs) {
		ctx, shutdown := initFn()
		err := funcs[ix](ctx, env, args)
		shutdown()
		return err
	}
	return ErrUnknownRunner
}

// RunnerFunc behaves similarly to cmdline.RunnerFunc, but takes a run function
// fn that includes a context as the first arg.  The context is created via
// v23.Init when Run is called on the returned Runner.
func RunnerFunc(fn func(*context.T, *cmdline2.Env, []string) error) cmdline2.Runner {
	ix := indexRunner(len(funcs))
	funcs = append(funcs, fn)
	return ix
}

// Lookup returns the function registered via RunnerFunc corresponding to
// runner, or nil if it doesn't exist.
func Lookup(runner cmdline2.Runner) func(*context.T, *cmdline2.Env, []string) error {
	if ix, ok := runner.(indexRunner); ok && int(ix) < len(funcs) {
		return funcs[ix]
	}
	return nil
}

// Run performs Lookup, and then runs the function with the given ctx, env and
// args.
//
// Doesn't call v23.Init; the context initialization is up to you.
func Run(runner cmdline2.Runner, ctx *context.T, env *cmdline2.Env, args []string) error {
	if fn := Lookup(runner); fn != nil {
		return fn(ctx, env, args)
	}
	return ErrUnknownRunner
}

// ParseAndRun parses the cmd with the given env and args, and calls Run to run
// the returned runner with the ctx, env and args.
//
// Doesn't call v23.Init; the context initialization is up to you.
func ParseAndRun(cmd *cmdline2.Command, ctx *context.T, env *cmdline2.Env, args []string) error {
	runner, args, err := cmdline2.Parse(cmd, env, args)
	if err != nil {
		return err
	}
	return Run(runner, ctx, env, args)
}

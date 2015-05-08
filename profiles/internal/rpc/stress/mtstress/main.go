// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"flag"
	"net"
	"os"
	"regexp"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
	_ "v.io/x/ref/profiles"
)

func init() {
	cmdline.HideGlobalFlagsExcept(regexp.MustCompile(`^((rate)|(duration)|(reauthenticate))$`))
}

var (
	gctx *context.T

	rate     = flag.Float64("rate", 1, "Rate, in RPCs per second, to send to the test server")
	duration = flag.Duration("duration", 10*time.Second, "Duration for sending test traffic and measuring latency")
	reauth   = flag.Bool("reauthenticate", false, "If true, establish a new authenticated connection for each RPC, simulating load from a distinct process")

	cmdMount = &cmdline.Command{
		Name:  "mount",
		Short: "Measure latency of the Mount RPC at a fixed request rate",
		Long: `
Repeatedly issues a Mount request (at --rate) and measures latency
`,
		ArgsName: "<mountpoint> <ttl>",
		ArgsLong: `
<mountpoint> defines the name to be mounted

<ttl> specfies the time-to-live of the mount point. For example: 5s for 5
seconds, 1m for 1 minute etc.
Valid time units are "ms", "s", "m", "h".
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if got, want := len(args), 2; got != want {
				return cmd.UsageErrorf("mount: got %d arguments, want %d", got, want)
			}
			mountpoint := args[0]
			ttl, err := time.ParseDuration(args[1])
			if err != nil {
				return cmd.UsageErrorf("invalid TTL: %v", err)
			}
			// Make up a random server to mount
			ep := naming.FormatEndpoint("tcp", "127.0.0.1:14141")
			mount := func(ctx *context.T) (time.Duration, error) {
				// Currently this is a simple call, but at some
				// point should generate random test data -
				// mountpoints at different depths and the like
				start := time.Now()
				if err := v23.GetClient(ctx).Call(ctx, mountpoint, "Mount", []interface{}{ep, uint32(ttl / time.Second), 0}, nil, options.NoResolve{}); err != nil {
					return 0, err
				}
				return time.Since(start), nil
			}
			p, err := paramsFromFlags(mountpoint)
			if err != nil {
				return err
			}
			return run(mount, p)
		},
	}

	cmdResolve = &cmdline.Command{
		Name:  "resolve",
		Short: "Measure latency of the Resolve RPC at a fixed request rate",
		Long: `
Repeatedly issues a Resolve request (at --rate) to a name and measures latency
`,
		ArgsName: "<name>",
		ArgsLong: `
<name> the object name to resolve
`,
		Run: func(cmd *cmdline.Command, args []string) error {
			if got, want := len(args), 1; got != want {
				return cmd.UsageErrorf("resolve: got %d arguments, want %d", got, want)
			}
			name := args[0]
			resolve := func(ctx *context.T) (time.Duration, error) {
				var entry naming.MountEntry
				start := time.Now()
				if err := v23.GetClient(ctx).Call(ctx, name, "ResolveStep", nil, []interface{}{&entry}, options.NoResolve{}); err != nil && verror.ErrorID(err) != naming.ErrNoSuchName.ID {
					// ErrNoSuchName is fine, it just means
					// that the mounttable server did not
					// find an entry in its tables.
					return 0, err
				}
				return time.Since(start), nil
			}
			p, err := paramsFromFlags(name)
			if err != nil {
				return err
			}
			return run(resolve, p)
		},
	}
)

func paramsFromFlags(objectName string) (params, error) {
	// Measure network distance to objectName
	const iters = 5
	addr, _ := naming.SplitAddressName(objectName)
	if len(addr) == 0 {
		addr, _ = naming.SplitAddressName(v23.GetNamespace(gctx).Roots()[0])
	}
	ep, err := v23.NewEndpoint(addr)
	if err != nil {
		return params{}, err
	}
	// Assume TCP
	var total time.Duration
	for i := 0; i < iters; i++ {
		start := time.Now()
		conn, err := net.Dial("tcp", ep.Addr().String())
		if err != nil {
			return params{}, err
		}
		total += time.Since(start)
		conn.Close()
	}
	return params{
		Rate:            *rate,
		Duration:        *duration,
		NetworkDistance: time.Duration(total.Nanoseconds() / iters),
		Context:         gctx,
		Reauthenticate:  *reauth,
	}, nil
}

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()
	gctx = ctx
	root := &cmdline.Command{
		Name:     "mtstress",
		Short:    "Tool to stress test a mounttable service by issuing a fixed rate of requests per second and measuring latency",
		Children: []*cmdline.Command{cmdMount, cmdResolve},
	}
	os.Exit(root.Main())
}

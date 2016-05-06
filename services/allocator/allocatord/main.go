// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"

	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/allocator"
)

var (
	nameFlag               string
	serverNameFlag         string
	deploymentTemplateFlag string
	maxInstancesFlag       int
	diskSizeFlag           string
	gcloudBinFlag          string
	vkubeBinFlag           string
	vkubeCfgFlag           string

	cmdRoot = &cmdline.Command{
		Runner: v23cmd.RunnerFunc(runAllocator),
		Name:   "allocatord",
		Short:  "Runs the allocator service",
		Long:   "Runs the allocator service",
	}
)

func main() {
	cmdRoot.Flags.StringVar(&nameFlag, "name", "", "Name to publish for this service.")
	cmdRoot.Flags.StringVar(&serverNameFlag, "server-name", "", "Name of the servers to allocate. This name is part of the published names in the Vanadium namespace and the names of the Deployments in Kubernetes.")
	cmdRoot.Flags.StringVar(&deploymentTemplateFlag, "deployment-template", "", "The template for the deployment of the servers to allocate.")
	cmdRoot.Flags.IntVar(&maxInstancesFlag, "max-instances", 10, "The maximum total number of server instances to create.")
	cmdRoot.Flags.StringVar(&diskSizeFlag, "server-disk-size", "50GB", "The size of the persistent disk to allocate with the servers.")
	cmdRoot.Flags.StringVar(&gcloudBinFlag, "gcloud", "gcloud", "The gcloud binary to use.")
	cmdRoot.Flags.StringVar(&vkubeBinFlag, "vkube", "vkube", "The vkube binary to use.")
	cmdRoot.Flags.StringVar(&vkubeCfgFlag, "vkube-cfg", "vkube.cfg", "The vkube.cfg to use.")
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdRoot)
}

func runAllocator(ctx *context.T, env *cmdline.Env, args []string) error {
	if len(serverNameFlag) > 30 {
		// The names in Kubernetes have to be 63 characters or less. We
		// use <server-name>-<md5> as name, where "-<md5>" is 33 chars.
		return env.UsageErrorf("--server-name value too long. Must be <= 30 characters")
	}
	ctx, server, err := v23.WithNewServer(
		ctx,
		nameFlag,
		allocator.AllocatorServer(&allocatorImpl{}),
		security.AllowEveryone(),
	)
	if err != nil {
		return err
	}
	ctx.Infof("Listening on: %v", server.Status().Endpoints)
	<-signals.ShutdownOnSignals(ctx)
	return nil
}

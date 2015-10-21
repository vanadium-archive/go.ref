// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"fmt"
	"strings"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/generic"
)

var (
	flagConfigFile   string
	flagKubectlBin   string
	flagGcloudBin    string
	flagResourceFile string
	flagVerbose      bool
)

func main() {
	cmdline.HideGlobalFlagsExcept()

	cmd := &cmdline.Command{
		Name:  "vkube",
		Short: "Manages Vanadium applications on kubernetes",
		Long:  "Manages Vanadium applications on kubernetes",
		Children: []*cmdline.Command{
			cmdGetCredentials,
			cmdStart,
			cmdUpdate,
			cmdStop,
			cmdStartClusterAgent,
			cmdStopClusterAgent,
			cmdClaimClusterAgent,
			cmdBuildDockerImages,
		},
	}
	cmd.Flags.StringVar(&flagConfigFile, "config", "vkube.cfg", "The 'vkube.cfg' file to use.")
	cmd.Flags.StringVar(&flagKubectlBin, "kubectl", "kubectl", "The 'kubectl' binary to use.")
	cmd.Flags.StringVar(&flagGcloudBin, "gcloud", "gcloud", "The 'gcloud' binary to use.")

	cmdStart.Flags.StringVar(&flagResourceFile, "f", "", "Filename to use to create the kubernetes resource.")

	cmdUpdate.Flags.StringVar(&flagResourceFile, "f", "", "Filename to use to update the kubernetes resource.")

	cmdStop.Flags.StringVar(&flagResourceFile, "f", "", "Filename to use to stop the kubernetes resource.")

	cmdBuildDockerImages.Flags.BoolVar(&flagVerbose, "v", false, "When true, the output is more verbose.")

	cmdline.Main(cmd)
}

var cmdGetCredentials = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runCmdGetCredentials),
	Name:   "get-credentials",
	Short:  "Gets the kubernetes credentials from Google Cloud.",
	Long:   "Gets the kubernetes credentials from Google Cloud.",
}

func runCmdGetCredentials(ctx *context.T, env *cmdline.Env, args []string) error {
	config, err := readConfig(flagConfigFile)
	if err != nil {
		return err
	}
	if config.Cluster == "" {
		return fmt.Errorf("Cluster must be set.")
	}
	if config.Project == "" {
		return fmt.Errorf("Project must be set.")
	}
	if config.Zone == "" {
		return fmt.Errorf("Zone must be set.")
	}
	return getCredentials(config.Cluster, config.Project, config.Zone)
}

var cmdStart = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(runCmdStart),
	Name:     "start",
	Short:    "Starts an application.",
	Long:     "Starts an application.",
	ArgsName: "<extension>",
	ArgsLong: "<extension> The blessing name extension to give to the application.",
}

func runCmdStart(ctx *context.T, env *cmdline.Env, args []string) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("start: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	extension := args[0]

	config, err := readConfig(flagConfigFile)
	if err != nil {
		return err
	}
	if flagResourceFile == "" {
		return fmt.Errorf("-f must be specified.")
	}
	rc, err := readReplicationControllerConfig(flagResourceFile)
	if err != nil {
		return err
	}
	for _, v := range []string{"spec.template.metadata.labels.application", "spec.template.metadata.labels.deployment"} {
		if rc.getString(v) == "" {
			fmt.Fprintf(env.Stderr, "WARNING: %q is not set. Rolling updates will not work.\n", v)
		}
	}
	agentAddr, err := findClusterAgent(config, true)
	if err != nil {
		return err
	}
	secretName, err := makeSecretName()
	if err != nil {
		return err
	}
	namespace := rc.getString("metadata.namespace")
	appName := rc.getString("spec.template.metadata.labels.application")
	if n, err := findReplicationControllerNameForApp(appName, namespace); err == nil {
		return fmt.Errorf("replication controller for application=%q already running: %s", appName, n)
	}
	if err := createSecret(ctx, secretName, namespace, agentAddr, extension); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Created Secret successfully.")

	if err := createReplicationController(ctx, config, rc, secretName); err != nil {
		if err := deleteSecret(ctx, config, secretName, namespace); err != nil {
			ctx.Error(err)
		}
		return err
	}
	fmt.Fprintln(env.Stdout, "Created replication controller successfully.")
	return nil
}

var cmdUpdate = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runCmdUpdate),
	Name:   "update",
	Short:  "Updates an application.",
	Long:   "Updates an application to a new version with a rolling update, preserving the existing blessings.",
}

func runCmdUpdate(ctx *context.T, env *cmdline.Env, args []string) error {
	config, err := readConfig(flagConfigFile)
	if err != nil {
		return err
	}
	if flagResourceFile == "" {
		return fmt.Errorf("-f must be specified.")
	}
	rc, err := readReplicationControllerConfig(flagResourceFile)
	if err != nil {
		return err
	}
	if err := updateReplicationController(ctx, config, rc); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Updated replication controller successfully.")
	return nil
}

var cmdStop = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runCmdStop),
	Name:   "stop",
	Short:  "Stops an application.",
	Long:   "Stops an application.",
}

func runCmdStop(ctx *context.T, env *cmdline.Env, args []string) error {
	config, err := readConfig(flagConfigFile)
	if err != nil {
		return err
	}
	if flagResourceFile == "" {
		return fmt.Errorf("-f must be specified.")
	}
	rc, err := readReplicationControllerConfig(flagResourceFile)
	if err != nil {
		return err
	}
	name := rc.getString("metadata.name")
	if name == "" {
		return fmt.Errorf("metadata.name must be set")
	}
	namespace := rc.getString("metadata.namespace")
	secretName, err := findSecretName(name, namespace)
	if err != nil {
		return err
	}
	if out, err := kubectl("--namespace="+namespace, "stop", "rc", name); err != nil {
		return fmt.Errorf("failed to stop replication controller: %v: %s", err, out)
	}
	fmt.Fprintf(env.Stdout, "Stopping replication controller.\n")
	if err := deleteSecret(ctx, config, secretName, namespace); err != nil {
		return fmt.Errorf("failed to delete Secret: %v", err)
	}
	fmt.Fprintf(env.Stdout, "Deleting Secret.\n")
	return nil
}

var cmdStartClusterAgent = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runCmdStartClusterAgent),
	Name:   "start-cluster-agent",
	Short:  "Starts the cluster agent.",
	Long:   "Starts the cluster agent.",
}

func runCmdStartClusterAgent(ctx *context.T, env *cmdline.Env, args []string) error {
	config, err := readConfig(flagConfigFile)
	if err != nil {
		return err
	}
	if err := createClusterAgent(ctx, config); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Starting Cluster Agent.\n")
	return nil
}

var cmdStopClusterAgent = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runCmdStopClusterAgent),
	Name:   "stop-cluster-agent",
	Short:  "Stops the cluster agent.",
	Long:   "Stops the cluster agent.",
}

func runCmdStopClusterAgent(ctx *context.T, env *cmdline.Env, args []string) error {
	config, err := readConfig(flagConfigFile)
	if err != nil {
		return err
	}
	if err := stopClusterAgent(config); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Stopping Cluster Agent.\n")
	return nil
}

var cmdClaimClusterAgent = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runCmdClaimClusterAgent),
	Name:   "claim-cluster-agent",
	Short:  "Claims the cluster agent.",
	Long:   "Claims the cluster agent.",
}

func runCmdClaimClusterAgent(ctx *context.T, env *cmdline.Env, args []string) error {
	config, err := readConfig(flagConfigFile)
	if err != nil {
		return err
	}
	myBlessings := v23.GetPrincipal(ctx).BlessingStore().Default()
	claimer := clusterAgentClaimer(config)
	if !myBlessings.CouldHaveNames([]string{claimer}) {
		return fmt.Errorf("principal isn't the expected claimer: got %q, expected %q", myBlessings, claimer)
	}
	extension := strings.TrimPrefix(config.ClusterAgent.Blessing, claimer+security.ChainSeparator)
	if err := claimClusterAgent(ctx, config, extension); err != nil {
		if verror.ErrorID(err) == verror.ErrUnknownMethod.ID {
			return fmt.Errorf("already claimed")
		}
		return err
	}
	fmt.Fprintf(env.Stdout, "Claimed Cluster Agent successfully.\n")
	return nil
}

var cmdBuildDockerImages = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runCmdBuildDockerImages),
	Name:   "build-docker-images",
	Short:  "Builds the docker images for the cluster and pod agents.",
	Long:   "Builds the docker images for the cluster and pod agents.",
}

func runCmdBuildDockerImages(ctx *context.T, env *cmdline.Env, args []string) error {
	config, err := readConfig(flagConfigFile)
	if err != nil {
		return err
	}
	return buildDockerImages(config, flagVerbose, env.Stdout)
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go .

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"

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
	flagTag          string
	flagWait         bool
)

func main() {
	cmdline.HideGlobalFlagsExcept()

	cmd := &cmdline.Command{
		Name:  "vkube",
		Short: "Manages Vanadium applications on kubernetes",
		Long:  "Manages Vanadium applications on kubernetes",
		Children: []*cmdline.Command{
			cmdStart,
			cmdUpdate,
			cmdStop,
			cmdStartClusterAgent,
			cmdStopClusterAgent,
			cmdClaimClusterAgent,
			cmdBuildDockerImages,
			cmdCtl,
		},
	}
	cmd.Flags.StringVar(&flagConfigFile, "config", "vkube.cfg", "The 'vkube.cfg' file to use.")
	cmd.Flags.StringVar(&flagKubectlBin, "kubectl", "kubectl", "The 'kubectl' binary to use.")
	cmd.Flags.StringVar(&flagGcloudBin, "gcloud", "gcloud", "The 'gcloud' binary to use.")

	cmdStart.Flags.StringVar(&flagResourceFile, "f", "", "Filename to use to create the kubernetes resource.")
	cmdStart.Flags.BoolVar(&flagWait, "wait", false, "Wait for at least one replica to be ready.")

	cmdUpdate.Flags.StringVar(&flagResourceFile, "f", "", "Filename to use to update the kubernetes resource.")
	cmdUpdate.Flags.BoolVar(&flagWait, "wait", false, "Wait for at least one replica to be ready after the update.")

	cmdStop.Flags.StringVar(&flagResourceFile, "f", "", "Filename to use to stop the kubernetes resource.")

	cmdStartClusterAgent.Flags.BoolVar(&flagWait, "wait", false, "Wait for the cluster agent to be ready.")

	cmdBuildDockerImages.Flags.BoolVar(&flagVerbose, "v", false, "When true, the output is more verbose.")
	cmdBuildDockerImages.Flags.StringVar(&flagTag, "tag", "", "The tag to add to the docker images. If empty, the current timestamp is used.")

	cmdline.Main(cmd)
}

func kubeCmdRunner(kcmd func(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error) cmdline.Runner {
	return v23cmd.RunnerFunc(func(ctx *context.T, env *cmdline.Env, args []string) error {
		config, err := readConfig(flagConfigFile)
		if err != nil {
			return err
		}
		f, err := ioutil.TempFile("", "kubeconfig-")
		if err != nil {
			return err
		}
		os.Setenv("KUBECONFIG", f.Name())
		defer os.Remove(f.Name())
		f.Close()

		if out, err := exec.Command(flagGcloudBin, "container", "clusters", "get-credentials", config.Cluster, "--project", config.Project, "--zone", config.Zone).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to get credentials for %q: %v: %s", config.Cluster, err, out)
		}
		return kcmd(ctx, env, args, config)
	})
}

var cmdStart = &cmdline.Command{
	Runner:   kubeCmdRunner(runCmdStart),
	Name:     "start",
	Short:    "Starts an application.",
	Long:     "Starts an application.",
	ArgsName: "<extension>",
	ArgsLong: "<extension> The blessing name extension to give to the application.",
}

func runCmdStart(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error {
	if expected, got := 1, len(args); expected != got {
		return env.UsageErrorf("start: incorrect number of arguments, expected %d, got %d", expected, got)
	}
	extension := args[0]
	if flagResourceFile == "" {
		return fmt.Errorf("-f must be specified.")
	}
	rc, err := readReplicationControllerConfig(flagResourceFile)
	if err != nil {
		return err
	}
	for _, v := range []string{"spec.template.metadata.labels.application", "spec.template.metadata.labels.version"} {
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
	if n, err := findReplicationControllerNamesForApp(appName, namespace); err != nil {
		return err
	} else if len(n) != 0 {
		return fmt.Errorf("replication controller for application=%q already running: %q", appName, n)
	}
	if err := createSecret(ctx, secretName, namespace, agentAddr, extension); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Created secret successfully.")

	if err := createReplicationController(ctx, config, rc, secretName); err != nil {
		if err := deleteSecret(ctx, config, secretName, namespace); err != nil {
			ctx.Error(err)
		}
		return err
	}
	fmt.Fprintln(env.Stdout, "Created replication controller successfully.")
	if flagWait {
		if err := waitForReadyPods(appName, namespace); err != nil {
			return err
		}
		fmt.Fprintln(env.Stdout, "Application is running.")
	}
	return nil
}

var cmdUpdate = &cmdline.Command{
	Runner: kubeCmdRunner(runCmdUpdate),
	Name:   "update",
	Short:  "Updates an application.",
	Long:   "Updates an application to a new version with a rolling update, preserving the existing blessings.",
}

func runCmdUpdate(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error {
	if flagResourceFile == "" {
		return fmt.Errorf("-f must be specified.")
	}
	rc, err := readReplicationControllerConfig(flagResourceFile)
	if err != nil {
		return err
	}
	if err := updateReplicationController(ctx, config, rc, env.Stdout, env.Stderr); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Updated replication controller successfully.")
	if flagWait {
		namespace := rc.getString("metadata.namespace")
		appName := rc.getString("spec.template.metadata.labels.application")
		if err := waitForReadyPods(appName, namespace); err != nil {
			return err
		}
		fmt.Fprintln(env.Stdout, "Application is running.")
	}
	return nil
}

var cmdStop = &cmdline.Command{
	Runner: kubeCmdRunner(runCmdStop),
	Name:   "stop",
	Short:  "Stops an application.",
	Long:   "Stops an application.",
}

func runCmdStop(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error {
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
	fmt.Fprintln(env.Stdout, "Stopping replication controller.")
	if err := deleteSecret(ctx, config, secretName, namespace); err != nil {
		return fmt.Errorf("failed to delete secret: %v", err)
	}
	fmt.Fprintln(env.Stdout, "Deleting secret.")
	return nil
}

var cmdStartClusterAgent = &cmdline.Command{
	Runner: kubeCmdRunner(runCmdStartClusterAgent),
	Name:   "start-cluster-agent",
	Short:  "Starts the cluster agent.",
	Long:   "Starts the cluster agent.",
}

func runCmdStartClusterAgent(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error {
	if err := createClusterAgent(ctx, config); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Starting cluster agent.")
	if flagWait {
		if err := waitForReadyPods(clusterAgentApplicationName, config.ClusterAgent.Namespace); err != nil {
			return err
		}
		for {
			if _, err := findClusterAgent(config, true); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		fmt.Fprintf(env.Stdout, "Cluster agent is ready.\n")
	}
	return nil
}

var cmdStopClusterAgent = &cmdline.Command{
	Runner: kubeCmdRunner(runCmdStopClusterAgent),
	Name:   "stop-cluster-agent",
	Short:  "Stops the cluster agent.",
	Long:   "Stops the cluster agent.",
}

func runCmdStopClusterAgent(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error {
	if err := stopClusterAgent(config); err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "Stopping cluster agent.")
	return nil
}

var cmdClaimClusterAgent = &cmdline.Command{
	Runner: kubeCmdRunner(runCmdClaimClusterAgent),
	Name:   "claim-cluster-agent",
	Short:  "Claims the cluster agent.",
	Long:   "Claims the cluster agent.",
}

func runCmdClaimClusterAgent(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error {
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
	fmt.Fprintln(env.Stdout, "Claimed cluster agent successfully.")
	return nil
}

var cmdBuildDockerImages = &cmdline.Command{
	Runner: kubeCmdRunner(runCmdBuildDockerImages),
	Name:   "build-docker-images",
	Short:  "Builds the docker images for the cluster and pod agents.",
	Long:   "Builds the docker images for the cluster and pod agents.",
}

func runCmdBuildDockerImages(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error {
	return buildDockerImages(config, flagTag, flagVerbose, env.Stdout)
}

var cmdCtl = &cmdline.Command{
	Runner:   kubeCmdRunner(runCmdCtl),
	Name:     "ctl",
	Short:    "Runs kubectl on the cluster defined in vkube.cfg.",
	Long:     "Runs kubectl on the cluster defined in vkube.cfg.",
	ArgsName: "-- <kubectl args>",
	ArgsLong: "<kubectl args> are passed directly to the kubectl command.",
}

func runCmdCtl(ctx *context.T, env *cmdline.Env, args []string, config *vkubeConfig) error {
	cmd := exec.Command(flagKubectlBin, args...)
	cmd.Stdin = env.Stdin
	cmd.Stdout = env.Stdout
	cmd.Stderr = env.Stderr
	return cmd.Run()
}

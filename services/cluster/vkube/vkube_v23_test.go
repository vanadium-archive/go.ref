// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"text/template"
	"time"

	"v.io/x/lib/textutil"
	"v.io/x/ref/test/testutil"
	"v.io/x/ref/test/v23test"
)

var (
	flagProject        = flag.String("project", "", "The name of the GCE project to use.")
	flagZone           = flag.String("zone", "", "The name of the GCE zone to use.")
	flagCluster        = flag.String("cluster", "", "The name of the kubernetes cluster to use.")
	flagGetCredentials = flag.Bool("get-credentials", true, "This flag is passed to vkube.")
	flagDockerRegistry = flag.String("docker-registry", "", "The docker registry to use. If empty, a temporary bucket in <project> is used.")
	flagRebuildProb    = flag.Float64("rebuild-probability", 0.05, "A number between 0 and 1 to control how often the docker images are rebuilt. A value of 0 means never. A value of 1 means always. This flag is only meaningful when --docker-registry is set.")
)

// TestV23Vkube is an end-to-end test for the vkube command. It operates on a
// pre-existing kubernetes cluster running on GCE.
// This test can easily exceed the default test timeout of 10m. It is
// recommended to use -test.timeout=20m.
func TestV23Vkube(t *testing.T) {
	if *flagProject == "" || (*flagGetCredentials && (*flagZone == "" || *flagCluster == "")) {
		t.Skip("--project, --zone, or --cluster not specified")
	}
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, nil)
	defer sh.Cleanup()

	workdir := sh.MakeTempDir()

	id := fmt.Sprintf("vkube-test-%08x", rand.Uint32())

	dockerRegistry := *flagDockerRegistry
	if dockerRegistry == "" {
		dockerRegistry = "b.gcr.io/" + id
	}

	vkubeCfgPath := filepath.Join(workdir, "vkube.cfg")
	if err := createVkubeConfig(vkubeCfgPath, id, dockerRegistry); err != nil {
		t.Fatal(err)
	}

	creds := sh.ForkCredentials("alice")

	vkubeBin := v23test.BuildGoPkg(sh, "v.io/x/ref/services/cluster/vkube")
	vshBin := v23test.BuildGoPkg(sh, "v.io/x/ref/examples/tunnel/vsh")

	var (
		cmd = func(name string, expectSuccess bool, baseArgs ...string) func(args ...string) string {
			return func(args ...string) string {
				args = append(baseArgs, args...)
				fmt.Printf("Running: %s %s\nExpect success: %v\n", name, strings.Join(args, " "), expectSuccess)
				// Note, creds do not affect non-Vanadium commands.
				c := sh.Cmd(name, args...).WithCredentials(creds)
				c.ExitErrorIsOk = true
				plw := textutil.PrefixLineWriter(os.Stdout, filepath.Base(name)+"> ")
				c.AddStdoutWriter(plw)
				c.AddStderrWriter(plw)
				output := c.CombinedOutput()
				plw.Flush()
				if expectSuccess && c.Err != nil {
					t.Error(testutil.FormatLogLine(2, "Unexpected failure: %s %s :%v", name, strings.Join(args, " "), c.Err))
				} else if !expectSuccess && c.Err == nil {
					t.Error(testutil.FormatLogLine(2, "Unexpected success %s: %s", name, strings.Join(args, " ")))
				}
				return output
			}
		}
		timedCmd = func(name string, d time.Duration, baseArgs ...string) func(args ...string) (string, error) {
			return func(args ...string) (string, error) {
				args = append(baseArgs, args...)
				fmt.Printf("Running: %s %s\n", name, strings.Join(args, " "))
				// Note, creds do not affect non-Vanadium commands.
				c := sh.Cmd(name, args...).WithCredentials(creds)
				c.ExitErrorIsOk = true
				plw := textutil.PrefixLineWriter(os.Stdout, fmt.Sprintf("%s(%s)> ", filepath.Base(name), d))
				// gosh.Cmd isn't thread-safe. We can't call c.CombinedOutput() in this goroutine and
				// c.Pid() in another one. Instead, we must use c.AddStdoutWriter(), c.AddStderrWriter(),
				// c.Start(), c.Pid(), and c.Wait(), all in the same goroutine. Then, use syscall.Kill()
				// in the other goroutine.
				var output bytes.Buffer
				c.AddStdoutWriter(&output)
				c.AddStderrWriter(&output)
				c.AddStdoutWriter(plw)
				c.AddStderrWriter(plw)
				c.Start()
				exit := make(chan struct{})
				go func(pid int) {
					select {
					case <-exit:
					case <-time.After(d):
						syscall.Kill(-pid, syscall.SIGINT)
						time.Sleep(time.Second)
						syscall.Kill(-pid, syscall.SIGKILL)
					}
				}(c.Pid())
				c.Wait()
				close(exit)
				plw.Flush()
				return output.String(), c.Err
			}
		}
		gsutil      = cmd("gsutil", true)
		gcloud      = cmd("gcloud", true, "--project="+*flagProject)
		docker      = cmd("docker", true)
		getCreds    = fmt.Sprintf("--get-credentials=%v", *flagGetCredentials)
		vkubeOK     = cmd(vkubeBin, true, "--config="+vkubeCfgPath, getCreds, "--no-headers")
		vkubeFail   = cmd(vkubeBin, false, "--config="+vkubeCfgPath, getCreds, "--no-headers")
		kubectlOK   = cmd(vkubeBin, true, "--config="+vkubeCfgPath, getCreds, "--no-headers", "kubectl", "--", "--namespace="+id)
		kubectlFail = cmd(vkubeBin, false, "--config="+vkubeCfgPath, getCreds, "--no-headers", "kubectl", "--", "--namespace="+id)
		kubectl5s   = timedCmd(vkubeBin, 5*time.Second, "--config="+vkubeCfgPath, getCreds, "--no-headers", "kubectl", "--", "--namespace="+id)
		vshOK       = cmd(vshBin, true)
	)

	if out := kubectlOK("cluster-info"); strings.Contains(out, "ERROR:") {
		// Exit early if we don't have valid credentials.
		t.Fatalf("Failed to get cluster information: %v", out)
	}

	if *flagDockerRegistry == "" {
		// Use a temporary bucket as docker registry.
		gsutil("mb", "-p", *flagProject, "gs://"+id)
		defer func() {
			gsutil("-m", "rm", "-r", "gs://"+id)
		}()
	}

	defer func() {
		kubectlOK("delete", "namespace", id)
	}()

	// Create app's docker image and configs.
	appImage := dockerRegistry + "/tunneld:latest"
	badImage := dockerRegistry + "/not-found"

	dockerDir, err := setupDockerDirectory(workdir)
	if err != nil {
		t.Fatal(err)
	}
	conf := make(map[string]string)
	for _, c := range []struct{ name, version, kind string }{
		{"app-rc1", "1", "rc"},
		{"app-rc2", "2", "rc"},
		{"app-dep1", "1", "deploy"},
		{"app-dep2", "2", "deploy"},
		{"app-dep-bad", "bad", "deploy-bad"},
		{"bb-rc1", "1", "busybox"},
		{"bb-rc2", "2", "busybox"},
	} {
		file := filepath.Join(workdir, c.name+".json")
		conf[c.name] = file
		var err error
		switch c.kind {
		case "rc":
			err = createAppReplicationControllerConfig(file, id, appImage, c.version)
		case "deploy":
			err = createAppDeploymentConfig(file, id, appImage, c.version)
		case "deploy-bad":
			err = createAppDeploymentConfig(file, id, badImage, c.version)
		case "busybox":
			err = createBusyboxConfig(file, id, c.version)
		default:
			err = fmt.Errorf("%s?", c.kind)
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	if *flagDockerRegistry == "" || rand.Float64() < *flagRebuildProb {
		t.Log("Rebuilding docker images")
		docker("build", "-t", appImage, dockerDir)
		gcloud("docker", "push", appImage)

		vkubeOK("build-docker-images", "-v", "-tag=1")
		vkubeOK("build-docker-images", "-v", "-tag=2")

		// Clean up local docker images.
		docker(
			"rmi",
			dockerRegistry+"/cluster-agent",
			dockerRegistry+"/cluster-agent:1",
			dockerRegistry+"/cluster-agent:2",
			dockerRegistry+"/pod-agent",
			dockerRegistry+"/pod-agent:1",
			dockerRegistry+"/pod-agent:2",
			dockerRegistry+"/tunneld",
		)
	}

	// Run the actual tests.
	vkubeOK("update-config",
		"--cluster-agent-image="+dockerRegistry+"/cluster-agent:1",
		"--pod-agent-image="+dockerRegistry+"/pod-agent:1")
	vkubeOK("start-cluster-agent", "--wait")
	vkubeOK("update-config", "--cluster-agent-image=:2", "--pod-agent-image=:2")
	vkubeOK("update-cluster-agent", "--wait")
	kubectlOK("get", "service", "cluster-agent")
	kubectlOK("get", "deployment", "cluster-agentd")
	kubectlOK("get", "rs,pods", "-Lversion")
	vkubeFail("start-cluster-agent") // Already running
	vkubeOK("claim-cluster-agent")
	vkubeFail("claim-cluster-agent") // Already claimed

	// App that uses ReplicationController
	vkubeOK("start", "-f", conf["app-rc1"], "--wait", "my-app")
	kubectlOK("get", "rc", "tunneld-1")
	vkubeFail("start", "-f", conf["app-rc1"], "my-app") // Already running

	vkubeOK("update", "-f", conf["app-rc2"], "--wait")
	kubectlOK("get", "rc", "tunneld-2")

	// Find the pod running tunneld, get the server's addr from its stdout.
	podName := kubectlOK("get", "pod", "-l", "application=tunneld,version=2", "--template={{range .items}}{{.metadata.name}}{{end}}")
	if podName == "" {
		t.Errorf("Failed to get pod name of tunneld")
	} else {
		var addr string
		for addr == "" {
			time.Sleep(100 * time.Millisecond)
			logs, err := kubectl5s("logs", podName, "-c", "tunneld")
			if err != nil {
				t.Logf("kubectl logs failed: %v", err)
				continue
			}
			for _, log := range strings.Split(logs, "\n") {
				if strings.HasPrefix(log, "NAME=") {
					addr = strings.TrimPrefix(log, "NAME=")
					break
				}
			}
		}
		if got, expected := vshOK(addr, "echo", "hello", "world"), "hello world\n"; got != expected {
			t.Errorf("Unexpected output. Got %q, expected %q", got, expected)
		}
	}

	vkubeOK("stop", "-f", conf["app-rc2"])
	kubectlFail("get", "rc", "tunneld-2")    // No longer running
	vkubeFail("stop", "-f", conf["app-rc2"]) // No longer running

	// App that uses Deployment
	vkubeOK("start", "-f", conf["app-dep1"], "--wait", "my-app")
	kubectlOK("get", "deployment", "tunneld")
	vkubeFail("start", "-f", conf["app-dep1"], "my-app") // Already running

	vkubeOK("update", "-f", conf["app-dep2"], "--wait")
	kubectlOK("describe", "deployment", "tunneld")
	kubectlOK("get", "pod", "--show-labels")

	// Find the pod running tunneld, get the server's addr from its stdout.
	podName = kubectlOK("get", "pod", "-l", "application=tunneld,version=2", "--template={{range .items}}{{.metadata.name}}{{end}}")
	if podName == "" {
		t.Errorf("Failed to get pod name of tunneld")
	} else {
		var addr string
		for addr == "" {
			time.Sleep(100 * time.Millisecond)
			logs, err := kubectl5s("logs", podName, "-c", "tunneld")
			if err != nil {
				t.Logf("kubectl logs failed: %v", err)
				continue
			}
			for _, log := range strings.Split(logs, "\n") {
				if strings.HasPrefix(log, "NAME=") {
					addr = strings.TrimPrefix(log, "NAME=")
					break
				}
			}
		}
		if got, expected := vshOK(addr, "echo", "hello", "world"), "hello world\n"; got != expected {
			t.Errorf("Unexpected output. Got %q, expected %q", got, expected)
		}
	}
	vkubeFail("update", "-f", conf["app-dep-bad"], "--wait", "--wait-timeout=30s")
	if out := kubectlOK("describe", "deployment", "tunneld"); !strings.Contains(out, "DeploymentRollback") {
		t.Error("expected a rollback in the deployment events")
	}

	vkubeOK("stop", "-f", conf["app-dep2"])
	kubectlFail("get", "deployment", "tunneld") // No longer running
	vkubeFail("stop", "-f", conf["app-dep2"])   // No longer running

	// App that uses Replication Controller, and no blessings.
	vkubeOK("start", "-f", conf["bb-rc1"], "--noblessings", "--wait")
	vkubeFail("start", "-f", conf["bb-rc1"], "--noblessings") // Already running
	vkubeOK("update", "-f", conf["bb-rc2"], "--wait")
	vkubeOK("stop", "-f", conf["bb-rc2"])
	vkubeFail("stop", "-f", conf["bb-rc2"]) // No longer running

	vkubeOK("stop-cluster-agent")
	kubectlFail("get", "service", "cluster-agent")
	kubectlFail("get", "deployment", "cluster-agentd")
}

func createVkubeConfig(path, id, dockerRegistry string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	params := struct{ Project, Zone, Cluster, Registry, ID string }{*flagProject, *flagZone, *flagCluster, dockerRegistry, id}
	return template.Must(template.New("cfg").Parse(`{
  "project": "{{.Project}}",
  "zone": "{{.Zone}}",
  "cluster": "{{.Cluster}}",
  "clusterAgent": {
    "namespace": "{{.ID}}",
    "image": "{{.Registry}}/cluster-agent:xxx",
    "blessing": "root:alice:cluster-agent",
    "admin": "root:alice",
    "cpu": "0.1",
    "memory": "100M"
  },
  "podAgent": {
    "image": "{{.Registry}}/pod-agent:xxx"
  }
}`)).Execute(f, params)
}

func setupDockerDirectory(workdir string) (string, error) {
	dockerDir := filepath.Join(workdir, "docker")
	if err := os.Mkdir(dockerDir, 0755); err != nil {
		return "", err
	}
	if err := ioutil.WriteFile(
		filepath.Join(dockerDir, "Dockerfile"),
		[]byte("FROM busybox\nCOPY tunneld /usr/local/bin/\n"),
		0644,
	); err != nil {
		return "", err
	}
	if out, err := exec.Command("jiri", "go", "build",
		"-o", filepath.Join(dockerDir, "tunneld"),
		"-ldflags", "-extldflags -static",
		"v.io/x/ref/examples/tunnel/tunneld").CombinedOutput(); err != nil {
		return "", fmt.Errorf("build failed: %v: %s", err, string(out))
	}
	return dockerDir, nil
}

func createAppReplicationControllerConfig(path, id, image, version string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	params := struct{ ID, Image, Version string }{id, image, version}
	return template.Must(template.New("appcfg").Parse(`{
  "apiVersion": "v1",
  "kind": "ReplicationController",
  "metadata": {
    "name": "tunneld-{{.Version}}",
    "namespace": "{{.ID}}",
    "labels": {
      "application": "tunneld"
    }
  },
  "spec": {
    "replicas": 1,
    "template": {
      "metadata": {
        "labels": {
          "application": "tunneld",
          "version": "{{.Version}}"
        }
      },
      "spec": {
        "containers": [
          {
            "name": "tunneld",
            "image": "{{.Image}}",
            "command": [
              "tunneld",
              "--v23.tcp.address=:8193",
              "--v23.permissions.literal={\"Admin\":{\"In\":[\"root:alice\"]}}",
	      "--alsologtostderr=false"
            ],
            "ports": [
              { "containerPort": 8193, "hostPort": 8193 }
            ],
            "resources": {
              "limits": { "cpu": "0.1", "memory": "100M" }
            }
          }
        ]
      }
    }
  }
}`)).Execute(f, params)
}

func createAppDeploymentConfig(path, id, image, version string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	params := struct{ ID, Image, Version string }{id, image, version}
	return template.Must(template.New("appcfg").Parse(`{
  "apiVersion": "extensions/v1beta1",
  "kind": "Deployment",
  "metadata": {
    "name": "tunneld",
    "namespace": "{{.ID}}",
    "labels": {
      "application": "tunneld"
    }
  },
  "spec": {
    "replicas": 1,
    "selector": {
      "matchLabels": {
        "application": "tunneld"
      }
    },
    "minReadySeconds": 5,
    "template": {
      "metadata": {
        "labels": {
          "application": "tunneld",
          "version": "{{.Version}}"
        }
      },
      "spec": {
        "containers": [
          {
            "name": "tunneld",
            "image": "{{.Image}}",
            "command": [
              "tunneld",
              "--v23.tcp.address=:8193",
              "--v23.permissions.literal={\"Admin\":{\"In\":[\"root:alice\"]}}",
	      "--alsologtostderr=false"
            ],
            "ports": [
              { "containerPort": 8193, "hostPort": 8193 }
            ],
            "readinessProbe": {
              "tcpSocket": { "port": 8193 },
              "initialDelaySeconds": 5,
              "timeoutSeconds": 1
            },
            "resources": {
              "limits": { "cpu": "0.1", "memory": "100M" }
            }
          }
        ]
      }
    }
  }
}`)).Execute(f, params)
}

func createBusyboxConfig(path, id, version string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	params := struct{ ID, Version string }{id, version}
	return template.Must(template.New("appcfg").Parse(`{
  "apiVersion": "v1",
  "kind": "ReplicationController",
  "metadata": {
    "name": "busybox-{{.Version}}",
    "namespace": "{{.ID}}",
    "labels": {
      "application": "busybox"
    }
  },
  "spec": {
    "replicas": 1,
    "template": {
      "metadata": {
        "labels": {
          "application": "busybox",
          "version": "{{.Version}}"
        }
      },
      "spec": {
        "containers": [
          {
            "name": "busybox",
            "image": "busybox",
            "command": [ "sleep", "3600" ]
          }
        ]
      }
    }
  }
}`)).Execute(f, params)
}

func TestMain(m *testing.M) {
	v23test.TestMain(m)
}

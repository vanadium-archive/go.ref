// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"v.io/x/lib/gosh"
	"v.io/x/lib/textutil"
	"v.io/x/ref/lib/v23test"
	"v.io/x/ref/test/testutil"
)

var (
	flagProject = flag.String("project", "", "The name of the GCE project to use.")
	flagZone    = flag.String("zone", "", "The name of the GCE zone to use.")
	flagCluster = flag.String("cluster", "", "The name of the kubernetes cluster to use.")
)

// TestV23Vkube is an end-to-end test for the vkube command. It operates on a
// pre-existing kubernetes cluster running on GCE.
// This test can easily exceed the default test timeout of 10m. It is
// recommended to use -test.timeout=20m.
func TestV23Vkube(t *testing.T) {
	if *flagProject == "" || *flagZone == "" || *flagCluster == "" {
		t.Skip("--project, --zone, or --cluster not specified")
	}
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	v23test.SkipUnlessRunningIntegrationTests(t)
	sh := v23test.NewShell(t, v23test.Opts{})
	defer sh.Cleanup()

	workdir := sh.MakeTempDir()

	id := fmt.Sprintf("vkube-test-%08x", rand.Uint32())

	vkubeCfgPath := filepath.Join(workdir, "vkube.cfg")
	if err := createVkubeConfig(vkubeCfgPath, id); err != nil {
		t.Fatal(err)
	}

	creds := sh.ForkCredentials("alice")

	vkubeBin := sh.BuildGoPkg("v.io/x/ref/services/cluster/vkube")
	vshBin := sh.BuildGoPkg("v.io/x/ref/examples/tunnel/vsh")

	var (
		cmd = func(name string, expectSuccess bool, baseArgs ...string) func(args ...string) string {
			return func(args ...string) string {
				args = append(baseArgs, args...)
				// Note, creds do not affect non-Vanadium commands.
				c := sh.Cmd(name, args...).WithCredentials(creds)
				c.ExitErrorIsOk = true
				prefix := textutil.PrefixLineWriter(os.Stdout, filepath.Base(name)+"> ")
				c.AddStdoutWriter(gosh.NopWriteCloser(prefix))
				stdout := c.Stdout()
				prefix.Flush()
				if expectSuccess && c.Err != nil {
					t.Error(testutil.FormatLogLine(2, "Unexpected failure: %s %s :%v", name, strings.Join(args, " "), c.Err))
				} else if !expectSuccess && c.Err == nil {
					t.Error(testutil.FormatLogLine(2, "Unexpected success %d: %s %s", name, strings.Join(args, " ")))
				}
				return stdout
			}
		}
		gsutil      = cmd("gsutil", true)
		gcloud      = cmd("gcloud", true, "--project="+*flagProject)
		docker      = cmd("docker", true)
		vkubeOK     = cmd(vkubeBin, true, "--config="+vkubeCfgPath)
		vkubeFail   = cmd(vkubeBin, false, "--config="+vkubeCfgPath)
		kubectlOK   = cmd(vkubeBin, true, "--config="+vkubeCfgPath, "ctl", "--", "--namespace="+id)
		kubectlFail = cmd(vkubeBin, false, "--config="+vkubeCfgPath, "ctl", "--", "--namespace="+id)
		vshOK       = cmd(vshBin, true)
	)

	if out := kubectlOK("cluster-info"); strings.Contains(out, "ERROR:") {
		// Exit early if we don't have valid credentials.
		t.Fatalf("Failed to get cluster information: %v", out)
	}

	// Create a bucket to store the docker images.
	gsutil("mb", "-p", *flagProject, "gs://"+id)
	defer func() {
		kubectlOK("delete", "namespace", id)
		gsutil("-m", "rm", "-R", "-a", "gs://"+id+"/*")
		gsutil("rb", "gs://"+id)
	}()

	// Create app's docker image and configs.
	dockerDir, err := setupDockerDirectory(workdir)
	if err != nil {
		t.Fatal(err)
	}
	appImage := "b.gcr.io/" + id + "/tunneld:latest"
	docker("build", "-t", appImage, dockerDir)
	gcloud("docker", "push", appImage)
	appConf1 := filepath.Join(workdir, "app1.json")
	if err := createAppConfig(appConf1, id, appImage, "1"); err != nil {
		t.Fatal(err)
	}
	appConf2 := filepath.Join(workdir, "app2.json")
	if err := createAppConfig(appConf2, id, appImage, "2"); err != nil {
		t.Fatal(err)
	}

	vkubeOK("build-docker-images", "-v", "-tag=test")
	// Clean up local docker images.
	docker(
		"rmi",
		"b.gcr.io/"+id+"/cluster-agent",
		"b.gcr.io/"+id+"/cluster-agent:test",
		"b.gcr.io/"+id+"/pod-agent",
		"b.gcr.io/"+id+"/pod-agent:test",
		"b.gcr.io/"+id+"/tunneld",
	)

	// Run the actual tests.
	vkubeOK("start-cluster-agent", "--wait")
	kubectlOK("get", "service", "cluster-agent")
	kubectlOK("get", "rc", "cluster-agentd-latest")
	vkubeOK("claim-cluster-agent")
	vkubeFail("start-cluster-agent") // Already running
	vkubeFail("claim-cluster-agent") // Already claimed

	vkubeOK("start", "-f", appConf1, "--wait", "my-app")
	kubectlOK("get", "rc", "tunneld-1")
	vkubeFail("start", "-f", appConf1, "my-app") // Already running

	vkubeOK("update", "-f", appConf2, "--wait")
	kubectlOK("get", "rc", "tunneld-2")

	// Find the pod running tunneld, get the server's addr from its stdout.
	podName := kubectlOK("get", "pod", "-l", "application=tunneld", "--template={{range .items}}{{.metadata.name}}{{end}}")
	addr := strings.TrimPrefix(strings.TrimSpace(kubectlOK("logs", podName, "-c", "tunneld")), "NAME=")
	if got, expected := vshOK(addr, "echo", "hello", "world"), "hello world\n"; got != expected {
		t.Errorf("Unexpected output. Got %q, expected %q", got, expected)
	}

	vkubeOK("stop", "-f", appConf2)
	kubectlFail("get", "rc", "tunneld-2") // No longer running
	vkubeFail("stop", "-f", appConf2)     // No longer running

	vkubeOK("stop-cluster-agent")
	kubectlFail("get", "service", "cluster-agent")
	kubectlFail("get", "rc", "cluster-agentd-latest")
}

func createVkubeConfig(path, id string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	params := struct{ Project, Zone, Cluster, ID string }{*flagProject, *flagZone, *flagCluster, id}
	return template.Must(template.New("cfg").Parse(`{
  "project": "{{.Project}}",
  "zone": "{{.Zone}}",
  "cluster": "{{.Cluster}}",
  "clusterAgent": {
    "namespace": "{{.ID}}",
    "image": "b.gcr.io/{{.ID}}/cluster-agent:latest",
    "blessing": "root:alice:cluster-agent",
    "admin": "root:alice",
    "cpu": "0.1",
    "memory": "100M"
  },
  "podAgent": {
    "image": "b.gcr.io/{{.ID}}/pod-agent:latest"
  }
}`)).Execute(f, params)
}

func setupDockerDirectory(workdir string) (string, error) {
	dockerDir := filepath.Join(workdir, "docker")
	if err := os.Mkdir(dockerDir, 0755); err != nil {
		return "", err
	}
	if err := ioutil.WriteFile(filepath.Join(dockerDir, "Dockerfile"), []byte(
		`FROM google/debian:wheezy
RUN apt-get update && apt-get install -y -qq --no-install-recommends libssl1.0.0 && apt-get clean
ADD tunneld /usr/local/bin/
`), 0644); err != nil {
		return "", err
	}
	if out, err := exec.Command("jiri", "go", "build", "-o", filepath.Join(dockerDir, "tunneld"), "v.io/x/ref/examples/tunnel/tunneld").CombinedOutput(); err != nil {
		return "", fmt.Errorf("build failed: %v: %s", err, string(out))
	}
	return dockerDir, nil
}

func createAppConfig(path, id, image, version string) error {
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

func TestMain(m *testing.M) {
	os.Exit(v23test.Run(m.Run))
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"strings"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vom"
	"v.io/x/ref/services/cluster"
)

// localAgentAddress returns the address of the cluster agent to use from within
// the cluster.
func localAgentAddress(config *vkubeConfig) string {
	return fmt.Sprintf("/(%s)@%s.%s:%d",
		config.ClusterAgent.Blessing,
		clusterAgentServiceName,
		config.ClusterAgent.Namespace,
		clusterAgentServicePort,
	)
}

// readReplicationControllerConfig reads a ReplicationController config from a
// file.
func readReplicationControllerConfig(fileName string) (object, error) {
	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	var rc object
	if err := rc.importJSON(data); err != nil {
		return nil, err
	}
	if kind := rc.getString("kind"); kind != "ReplicationController" {
		return nil, fmt.Errorf("expected kind=\"ReplicationController\", got %q", kind)
	}
	return rc, nil
}

// addPodAgent takes either a ReplicationController or Pod object and adds a
// pod-agent container to it. The existing containers are updated to use the
// pod agent.
func addPodAgent(ctx *context.T, config *vkubeConfig, obj object, secretName, rootBlessings string) error {
	var base string
	switch kind := obj.getString("kind"); kind {
	case "ReplicationController":
		base = "spec.template."
	case "Pod":
		base = ""
	default:
		return fmt.Errorf("expected kind=\"ReplicationController\" or \"Pod\", got %q", kind)
	}

	// Add the volumes used by the pod agent container.
	if err := obj.append(base+"spec.volumes",
		object{"name": "agent-logs", "emptyDir": object{}},
		object{"name": "agent-secret", "secret": object{"secretName": secretName}},
		object{"name": "agent-socket", "emptyDir": object{}},
	); err != nil {
		return err
	}

	// Update the existing containers to talk to the pod agent.
	containers := obj.getObjectArray(base + "spec.containers")
	for _, c := range containers {
		if err := c.append("env", object{"name": "V23_AGENT_PATH", "value": "/agent/socket/agent.sock"}); err != nil {
			return err
		}
		if err := c.append("volumeMounts", object{"name": "agent-socket", "mountPath": "/agent/socket", "readOnly": true}); err != nil {
			return err
		}
	}

	// Add the pod agent container.
	containers = append(containers, object{
		"name":  "pod-agent",
		"image": config.PodAgent.Image,
		"env": []object{
			object{"name": "ROOT_BLESSINGS", "value": rootBlessings},
		},
		"args": []string{
			"pod_agentd",
			"--agent=" + localAgentAddress(config),
			"--root-blessings=$(ROOT_BLESSINGS)",
			"--secret-key-file=/agent/secret/secret",
			"--socket-path=/agent/socket/agent.sock",
			"--log_dir=/logs",
		},
		"livenessProbe": object{
			"exec": object{"command": []string{
				"env", "V23_AGENT_PATH=/agent/socket/agent.sock", "principal", "dump",
			}},
			"initialDelaySeconds": 5,
			"timeoutSeconds":      1,
		},
		"volumeMounts": []object{
			object{"name": "agent-logs", "mountPath": "/logs"},
			object{"name": "agent-secret", "mountPath": "/agent/secret", "readOnly": true},
			object{"name": "agent-socket", "mountPath": "/agent/socket"},
		},
	})
	return obj.set(base+"spec.containers", containers)
}

// createSecret gets a new secret key from the cluster agent, and then creates a
// Secret object on kubernetes with it.
func createSecret(ctx *context.T, secretName, namespace, agentAddr, extension string) error {
	secret, err := cluster.ClusterAgentAdminClient(agentAddr).NewSecret(ctx, &granter{extension: extension})
	if err != nil {
		return err
	}
	if out, err := kubectlCreate(object{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": object{
			"name":      secretName,
			"namespace": namespace,
		},
		"type": "Opaque",
		"data": object{
			"secret": base64.StdEncoding.EncodeToString([]byte(secret)),
		},
	}); err != nil {
		return fmt.Errorf("failed to create secret %q: %v\n%s\n", secretName, err, string(out))
	}
	return nil
}

type granter struct {
	rpc.CallOpt
	extension string
}

func (g *granter) Grant(ctx *context.T, call security.Call) (security.Blessings, error) {
	p := call.LocalPrincipal()
	b, _ := p.BlessingStore().Default()
	return p.Bless(call.RemoteBlessings().PublicKey(), b, g.extension, security.UnconstrainedUse())
}

// deleteSecret deletes a Secret object and its associated secret key and
// blessings.
// We know the name of the Secret object, but we don't know the secret key. The
// only way to get it back from Kubernetes is to mount the Secret Object to a
// Pod, and then use the secret key to delete the secret key.
func deleteSecret(ctx *context.T, config *vkubeConfig, name, rootBlessings, namespace string) error {
	podName := fmt.Sprintf("delete-secret-%s", name)
	del := object{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": object{
			"name":      podName,
			"namespace": namespace,
		},
		"spec": object{
			"containers": []interface{}{
				object{
					"name":  "delete-secret",
					"image": config.ClusterAgent.Image,
					"args": []string{
						"/bin/bash",
						"-c",
						"cluster_agent --agent='" + localAgentAddress(config) + "' forget $(cat /agent/secret/secret) && /google-cloud-sdk/bin/kubectl --namespace=" + namespace + " delete secret " + name + " && /google-cloud-sdk/bin/kubectl --namespace=" + namespace + " delete pod " + podName,
					},
					"volumeMounts": []interface{}{
						object{"name": "agent-secret", "mountPath": "/agent/secret", "readOnly": true},
					},
				},
			},
			"restartPolicy":         "OnFailure",
			"activeDeadlineSeconds": 300,
		},
	}
	if err := addPodAgent(ctx, config, del, name, rootBlessings); err != nil {
		return err
	}
	out, err := kubectlCreate(del)
	if err != nil {
		return fmt.Errorf("failed to create delete Pod: %v: %s", err, out)
	}
	return nil
}

// createReplicationController takes a ReplicationController object, adds a
// pod-agent, and then creates it on kubernetes.
func createReplicationController(ctx *context.T, config *vkubeConfig, rc object, secretName string) error {
	if err := addPodAgent(ctx, config, rc, secretName, rootBlessings(ctx)); err != nil {
		return err
	}
	if out, err := kubectlCreate(rc); err != nil {
		return fmt.Errorf("failed to create replication controller: %v\n%s\n", err, string(out))
	}
	return nil
}

// updateReplicationController takes a ReplicationController object, adds a
// pod-agent (if needed), and then performs a rolling update.
func updateReplicationController(ctx *context.T, config *vkubeConfig, rc object, stdout, stderr io.Writer) error {
	namespace := rc.getString("metadata.namespace")
	oldNames, err := findReplicationControllerNamesForApp(rc.getString("spec.template.metadata.labels.application"), namespace)
	if err != nil {
		return err
	}
	if len(oldNames) != 1 {
		return fmt.Errorf("found %d replication controllers for this application: %q", len(oldNames), oldNames)
	}
	if oldNames[0] == rc.getString("metadata.name") {
		// Assume this update is a no op.
		fmt.Fprintf(stderr, "replication controller %q already exists\n", oldNames[0])
		return nil
	}
	secretName, rootBlessings, err := findPodAttributes(oldNames[0], namespace)
	if err != nil {
		return err
	}
	if secretName != "" {
		if err := addPodAgent(ctx, config, rc, secretName, rootBlessings); err != nil {
			return err
		}
	}
	if hasPersistentDisk(rc) {
		// Rolling updates with persistent disks don't work because
		// the new Pod cannot come up until the old one has released
		// the disk. Instead, we create the new RC and delete the old
		// one.
		if out, err := kubectlCreate(rc); err != nil {
			return fmt.Errorf("failed to create replication controller: %v: %s", err, string(out))
		}
		if out, err := kubectl("delete", "rc", oldNames[0], "--namespace="+namespace); err != nil {
			return fmt.Errorf("failed to delete replication controller %q: %v: %s", oldNames[0], err, string(out))
		}
		return nil
	}
	json, err := rc.json()
	if err != nil {
		return err
	}
	cmd := exec.Command(flagKubectlBin, "rolling-update", oldNames[0], "-f", "-", "--namespace="+namespace)
	cmd.Stdin = bytes.NewBuffer(json)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update replication controller %q: %v\n", oldNames[0], err)
	}
	return nil
}

func hasPersistentDisk(obj object) bool {
	for _, v := range obj.getObjectArray("spec.template.spec.volumes") {
		if v.get("gcePersistentDisk") != nil {
			return true
		}
	}
	return false
}

// createNamespaceIfNotExist creates a Namespace object if it doesn't already exist.
func createNamespaceIfNotExist(name string) error {
	if _, err := kubectl("get", "namespace", name); err == nil {
		return nil
	}
	if out, err := kubectlCreate(object{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": object{
			"name": name,
		},
	}); err != nil {
		return fmt.Errorf("failed to create Namespace %q: %v: %s", name, err, out)
	}
	return nil
}

// makeSecretName creates a random name for a Secret Object.
func makeSecretName() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("secret-%s", hex.EncodeToString(b)), nil
}

// findReplicationControllerNamesForApp returns the names of the
// ReplicationController that are currently used to run the given application.
func findReplicationControllerNamesForApp(app, namespace string) ([]string, error) {
	data, err := kubectl("--namespace="+namespace, "get", "rc", "-l", "application="+app, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to get replication controller for application %q: %v\n%s\n", app, err, string(data))
	}
	var list object
	if err := list.importJSON(data); err != nil {
		return nil, fmt.Errorf("failed to parse kubectl output: %v", err)
	}
	names := []string{}
	for _, item := range list.getObjectArray("items") {
		names = append(names, item.getString("metadata.name"))
	}
	return names, nil
}

// findPodAttributes finds the name of the Secret object and root blessings
// associated the given Replication Controller.
func findPodAttributes(rcName, namespace string) (string, string, error) {
	data, err := kubectl("--namespace="+namespace, "get", "rc", rcName, "-o", "json")
	if err != nil {
		return "", "", fmt.Errorf("failed to get replication controller %q: %v\n%s\n", rcName, err, string(data))
	}
	var rc object
	if err := rc.importJSON(data); err != nil {
		return "", "", fmt.Errorf("failed to parse kubectl output: %v", err)
	}

	// Find secret.
	var secret string
	for _, v := range rc.getObjectArray("spec.template.spec.volumes") {
		if v.getString("name") == "agent-secret" {
			secret = v.getString("secret.secretName")
			break
		}
	}

	// Find root blessings.
	var root string
L:
	for _, c := range rc.getObjectArray("spec.template.spec.containers") {
		if c.getString("name") == "pod-agent" {
			for _, e := range c.getObjectArray("env") {
				if e.getString("name") == "ROOT_BLESSINGS" {
					root = e.getString("value")
					break L
				}
			}
		}
	}
	return secret, root, nil
}

func readyPods(appName, namespace string) ([]string, error) {
	data, err := kubectl("--namespace="+namespace, "get", "pod", "-l", "application="+appName, "-o", "json")
	if err != nil {
		return nil, err
	}
	var list object
	if err := list.importJSON(data); err != nil {
		return nil, fmt.Errorf("failed to parse kubectl output: %v", err)
	}
	names := []string{}
	for _, item := range list.getObjectArray("items") {
		if item.get("status.phase") != "Running" {
			continue
		}
		for _, cond := range item.getObjectArray("status.conditions") {
			if cond.get("type") == "Ready" && cond.get("status") == "True" {
				names = append(names, item.getString("metadata.name"))
				break
			}
		}
		for _, status := range item.getObjectArray("status.containerStatuses") {
			if status.get("ready") == false && status.getInt("restartCount") >= 5 {
				return nil, fmt.Errorf("application is failing: %#v", item)
			}
		}
	}
	return names, nil
}

func waitForReadyPods(appName, namespace string) error {
	for {
		if n, err := readyPods(appName, namespace); err != nil {
			return err
		} else if len(n) > 0 {
			return nil
		}
		time.Sleep(time.Second)
	}
}

// kubectlCreate runs 'kubectl create -f' on the given object and returns the
// output.
func kubectlCreate(o object) ([]byte, error) {
	json, err := o.json()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(flagKubectlBin, "create", "-f", "-")
	cmd.Stdin = bytes.NewBuffer(json)
	return cmd.CombinedOutput()
}

// kubectl runs the 'kubectl' command with the given arguments and returns the
// output.
func kubectl(args ...string) ([]byte, error) {
	return exec.Command(flagKubectlBin, args...).CombinedOutput()
}

// rootBlessings returns the root blessings for the current principal.
func rootBlessings(ctx *context.T) string {
	p := v23.GetPrincipal(ctx)
	b, _ := p.BlessingStore().Default()
	b64 := []string{}
	for _, root := range security.RootBlessings(b) {
		data, err := vom.Encode(root)
		if err != nil {
			ctx.Fatalf("vom.Encode failed: %v", err)
		}
		// We use URLEncoding to be compatible with the principal
		// command.
		b64 = append(b64, base64.URLEncoding.EncodeToString(data))
	}
	return strings.Join(b64, ",")
}

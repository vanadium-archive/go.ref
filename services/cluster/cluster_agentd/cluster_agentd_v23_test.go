// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate jiri test generate

package main_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"v.io/v23/security"
	"v.io/x/ref"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/v23tests"
)

func V23TestClusterAgentD(t *v23tests.T) {
	workdir, err := ioutil.TempDir("", "cluster-agentd-test-")
	if err != nil {
		t.Fatalf("ioutil.TempDir failed: %v", err)
	}
	defer os.RemoveAll(workdir)

	agentCreds, err := t.Shell().NewChildCredentials("agent")
	if err != nil {
		t.Fatalf("Failed to create agent credentials: %v", err)
	}
	aliceCreds, err := t.Shell().NewChildCredentials("alice")
	if err != nil {
		t.Fatalf("Failed to create alice credentials: %v", err)
	}
	// Create a blessing (root/alice/prod) that alice will use to talk to
	// the cluster agent.
	alicePrincipal := aliceCreds.Principal()
	if prodBlessing, err := alicePrincipal.Bless(alicePrincipal.PublicKey(), alicePrincipal.BlessingStore().Default(), "prod", security.UnconstrainedUse()); err != nil {
		t.Fatalf("Failed to create alice/prod blessing: %v", err)
	} else if _, err := alicePrincipal.BlessingStore().Set(prodBlessing, security.BlessingPattern("root/agent")); err != nil {
		t.Fatalf("Failed to set alice/prod for root/agent: %v", err)
	}

	var (
		agentBin     = t.BuildV23Pkg("v.io/x/ref/services/cluster/cluster_agentd")
		clientBin    = t.BuildV23Pkg("v.io/x/ref/services/cluster/cluster_agent")
		podAgentBin  = t.BuildV23Pkg("v.io/x/ref/services/agent/pod_agentd")
		principalBin = t.BuildV23Pkg("v.io/x/ref/cmd/principal")
	)

	// Start the cluster agent.
	addr := agentBin.WithStartOpts(agentBin.StartOpts().WithCustomCredentials(agentCreds)).Start(
		"--v23.tcp.address=127.0.0.1:0",
		"--v23.permissions.literal={\"Admin\":{\"In\":[\"root/alice/prod\"]}}",
		"--root-dir="+workdir,
	).ExpectVar("NAME")

	// Create a new secret.
	secret := strings.TrimSpace(clientBin.WithStartOpts(clientBin.StartOpts().WithCustomCredentials(aliceCreds)).Start(
		"--agent="+addr,
		"new",
		"foo",
	).Output())
	secretPath := filepath.Join(workdir, "secret")
	if err := ioutil.WriteFile(secretPath, []byte(secret), 0600); err != nil {
		t.Fatalf("Unexpected WriteFile error: %v", err)
	}

	// Start the pod agent.
	sockPath := filepath.Join(workdir, "agent.sock")
	podAgentBin.Start(
		"--agent="+addr,
		"--socket-path="+sockPath,
		"--secret-key-file="+secretPath,
		"--root-blessings="+rootBlessings(t, agentCreds),
	)

	// Wait for the socket to show up.
	// TODO(rthellend): This should be fixed in agentlib.
	for c := 0; ; c++ {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		if c < 10 {
			time.Sleep(time.Second)
			continue
		}
		t.Fatalf("%q still doesn't exist after 10 sec", sockPath)
	}

	principalBin = principalBin.WithEnv(ref.EnvAgentPath + "=" + sockPath)
	opts := principalBin.StartOpts()
	// With ExecProtocol true, the v23_test library overrides the principal
	// in such a way that EnvAgentPath is not used.
	opts.ExecProtocol = false
	principalBin = principalBin.WithStartOpts(opts)

	// The principal served by the pod agent should have a blessing name
	// that starts with root/alice/foo/.
	if got, expected := principalBin.Start("dump", "-s").Output(), "root/alice/foo/"; !strings.HasPrefix(got, expected) {
		t.Errorf("Unexpected output. Got %q, expected %q", got, expected)
	}

	// Bob should not be able to call NewSecret.
	bobCreds, err := t.Shell().NewChildCredentials("bob")
	if err != nil {
		t.Fatalf("Failed to create bob credentials: %v", err)
	}
	clientBin = clientBin.WithStartOpts(clientBin.StartOpts().WithCustomCredentials(bobCreds))
	if err := clientBin.Start("--agent="+addr, "new", "foo").Wait(nil, nil); err == nil {
		t.Error("Unexpected success. Bob should not be able to call NewSecret")
	}

	// After Alice calls ForgetSecret, SeekBlessings no longer works.
	clientBin = clientBin.WithStartOpts(clientBin.StartOpts().WithCustomCredentials(aliceCreds))
	if err := clientBin.Start("--agent="+addr, "forget", secret).Wait(nil, nil); err != nil {
		t.Errorf("Unexpected forget error: %v", err)
	}
	if err := clientBin.Start("--agent="+addr, "seekblessings", secret).Wait(nil, nil); err == nil {
		t.Error("Unexpected success. This secret should not exist anymore.")
	}

	// The pod agent should be unaffected.
	if got, expected := principalBin.Start("dump", "-s").Output(), "root/alice/foo/"; !strings.HasPrefix(got, expected) {
		t.Errorf("Unexpected output. Got %q, expected %q", got, expected)
	}
}

func rootBlessings(t *v23tests.T, creds *modules.CustomCredentials) string {
	principalBin := t.BuildV23Pkg("v.io/x/ref/cmd/principal")
	opts := principalBin.StartOpts().WithCustomCredentials(creds)
	principalBin = principalBin.WithStartOpts(opts)
	blessings := strings.TrimSpace(principalBin.Start("get", "default").Output())

	opts.Stdin = bytes.NewBufferString(blessings)
	opts.ExecProtocol = false
	opts.Credentials = nil
	principalBin = principalBin.WithStartOpts(opts)
	out := strings.TrimSpace(principalBin.Start("dumproots", "-").Output())
	return strings.Replace(out, "\n", ",", -1)
}

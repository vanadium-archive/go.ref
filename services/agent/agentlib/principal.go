// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agentlib

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"v.io/v23/verror"
	"v.io/x/lib/metadata"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/constants"
	"v.io/x/ref/services/agent/version"
)

var (
	errNotADirectory = verror.Register(pkgPath+".errNotADirectory", verror.NoRetry, "{1:}{2:} {3} is not a directory{:_}")
	errCantCreate    = verror.Register(pkgPath+".errCantCreate", verror.NoRetry, "{1:}{2:} failed to create {3}{:_}")
	errFilepathAbs   = verror.Register(pkgPath+".errAbsFailed", verror.NoRetry, "{1:}{2:} filepath.Abs failed for {3}")
	errFindAgent     = verror.Register(pkgPath+".errFindAgent", verror.NoRetry, "{1:}{2:} couldn't find a suitable agent binary{:_}")
	errLaunchAgent   = verror.Register(pkgPath+".errLaunchAgent", verror.NoRetry, "{1:}{2:} couldn't launch agent{:_}")
)

// TODO(caprita): If we fail to run an external agent, we should consider
// running an in-process agent to serve the principal.  This can be just for the
// current process, but eventually we can allow other processes to connect to it
// as well (we'll need to have reconnect logic in the client that understands to
// start a new agent when the old one dies).

// TODO(caprita): The agent is expected to be up for the entire lifetime of a
// client; however, it should be possible for the client app to survive an agent
// death by having the app restart the agent and reconnecting. The main barrier
// to making the agent restartable by clients dynamically is the requirement to
// fetch the principal decryption passphrase over stdin.  Look into using
// something like pinentry.

// LoadPrincipal loads a principal (private key, BlessingRoots, BlessingStore)
// from the provided directory using the security agent.  If an agent serving
// the principal is not present, a new one is started as a separate daemon
// process.  The new agent may use os.Stdin and os.Stdout in order to fetch a
// private key decryption passphrase.  If an agent serving the principal is not
// found and a new one cannot be started, an error is returned.  The caller
// should call Close on the returned Principal once it's no longer used, in
// order to free up resources and allow the agent to terminate once it has no
// more clients.
func LoadPrincipal(credsDir string) (agent.Principal, error) {
	if finfo, err := os.Stat(credsDir); err != nil || err == nil && !finfo.IsDir() {
		return nil, verror.New(errNotADirectory, nil, credsDir)
	}
	sockPath := constants.SocketPath(credsDir)
	if path, err := filepath.Abs(sockPath); err != nil {
		return nil, verror.New(errFilepathAbs, nil, sockPath, err)
	} else {
		sockPath = path
	}
	sockPath = filepath.Clean(sockPath)

	// Since we don't hold a lock between launchAgent and
	// NewAgentPrincipalX, the agent could go away by the time we try to
	// connect to it (e.g. it decides there are no clients and exits
	// voluntarily); even if we held a lock, the agent could still crash in
	// the meantime.  Therefore, we retry a few times before giving up.
	tries, maxTries := 0, 5
	var (
		agentBin     string
		agentVersion version.T
	)
	for {
		// If the socket exists and we're able to connect over the
		// socket to an existing agent, we're done.  This works even if
		// the client only has access to the socket but no write access
		// to the credentials dir or agent dir required in order to
		// launch an agent.
		if p, err := NewAgentPrincipalX(sockPath); err == nil {
			return p, nil
		} else if tries == maxTries {
			return nil, err
		}
		// Go ahead and try to launch an agent.  Implicitly requires the
		// current process to have write permissions to the credentials
		// directory.
		tries++
		if agentBin == "" {
			var err error
			if agentBin, agentVersion, err = findAgent(); err != nil {
				return nil, verror.New(errFindAgent, nil, err)
			}
		}
		if err := launchAgent(credsDir, agentBin, agentVersion); err != nil {
			return nil, verror.New(errLaunchAgent, nil, err)
		}
	}
}

const agentBinName = "v23agentd"

// findAgent tries to locate an agent binary that we can run.
func findAgent() (string, version.T, error) {
	// TODO(caprita): Besides checking PATH, we can also look under
	// JIRI_ROOT.  Also, consider caching a copy of the agent binary under
	// <creds dir>/agent?
	var defaultVersion version.T
	agentBin, err := exec.LookPath(agentBinName)
	if err != nil {
		return "", defaultVersion, fmt.Errorf("failed to find %v: %v", agentBinName, err)
	}
	// Verify that the binary we found understands the "--metadata" flag, as
	// a simple check that it's executable and is a Vanadium binary.
	//
	// TODO(caprita): Consider verifying that the agent binary is
	// protocol-compatible with the current process (to avoid running one
	// that's too old).
	cmd := exec.Command(agentBin, "--metadata")
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Dir = os.TempDir()
	if err = cmd.Start(); err != nil {
		return "", defaultVersion, fmt.Errorf("failed to run %v: %v", cmd.Args, err)
	}
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()
	timeout := 5 * time.Second
	select {
	case err := <-waitErr:
		if err != nil {
			return "", defaultVersion, fmt.Errorf("%v failed: %v", cmd.Args, err)
		}
	case <-time.After(timeout):
		if err := cmd.Process.Kill(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to kill %v (PID:%d): %v\n", cmd.Args, cmd.Process.Pid, err)
		}
		if err := <-waitErr; err != nil {
			fmt.Fprintf(os.Stderr, "%v failed: %v\n", cmd.Args, err)
		}
		return "", defaultVersion, fmt.Errorf("%v failed to complete in %v", cmd.Args, timeout)
	}
	md, err := metadata.FromXML(b.Bytes())
	if err != nil {
		return "", defaultVersion, fmt.Errorf("%v output failed to parse as XML metadata: %v", cmd.Args, err)
	}
	versionRange, err := version.RangeFromString(nil, md.Lookup("v23agentd.VersionMin"), md.Lookup("v23agentd.VersionMax"))
	if err != nil {
		return "", defaultVersion, fmt.Errorf("%v output does not contain a valid version range: %v", cmd.Args, err)
	}
	versionToUse, err := version.Common(nil, versionRange, version.Supported)
	if err != nil {
		return "", defaultVersion, fmt.Errorf("%v version incompatible: %v", cmd.Args, err)
	}
	return agentBin, versionToUse, nil
}

// launchAgent launches the agent as a separate process and waits for it to
// serve the principal.
func launchAgent(credsDir, agentBin string, agentVersion version.T) error {
	agentRead, agentWrite, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %v", err)
	}
	defer agentRead.Close()
	cmd := exec.Command(agentBin, fmt.Sprintf("--with-version=%v", agentVersion), credsDir)
	agentDir := constants.AgentDir(credsDir)
	cmd.Dir = agentDir
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		return verror.New(errCantCreate, nil, agentDir, err)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = new(syscall.SysProcAttr)
	cmd.SysProcAttr.Setsid = true
	// The agentRead/Write pipe is used to get notification from the agent
	// when it's ready to serve the principal.
	cmd.ExtraFiles = append(cmd.ExtraFiles, agentWrite)
	cmd.Env = []string{"V23_AGENT_PARENT_PIPE_FD=3", "PATH=" + os.Getenv("PATH")}
	err = cmd.Start()
	agentWrite.Close()
	if err != nil {
		return fmt.Errorf("failed to run %v: %v", cmd.Args, err)
	}
	fmt.Fprintf(os.Stderr, "Started agent for credentials %v with PID %d\n", credsDir, cmd.Process.Pid)
	scanner := bufio.NewScanner(agentRead)
	if !scanner.Scan() || scanner.Text() != constants.ServingMsg {
		return fmt.Errorf("failed to receive \"%s\" from agent", constants.ServingMsg)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed reading status from agent: %v", err)
	}
	return nil
}

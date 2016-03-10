// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"v.io/v23/security"
	"v.io/x/lib/cmdline"
	"v.io/x/lib/metadata"
	"v.io/x/ref"
	vsignals "v.io/x/ref/lib/signals"
	"v.io/x/ref/services/agent/constants"
	"v.io/x/ref/services/agent/internal/lock"
	"v.io/x/ref/services/agent/server"
	"v.io/x/ref/services/agent/version"
)

const idleGrace = time.Minute

func init() {
	metadata.Insert("v23agentd.VersionMin", version.Supported.Min.String())
	metadata.Insert("v23agentd.VersionMax", version.Supported.Max.String())
}

var versionToUse = version.Supported.Max

func main() {
	cmdAgentD.Flags.Var(&versionToUse, "with-version", "Version that the agent should use.  Will fail if the version is not in the range of supported versions (obtained from the --metadata flag)")
	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdAgentD)
}

var cmdAgentD = &cmdline.Command{
	Runner: cmdline.RunnerFunc(runAgentD),
	Name:   "v23agentd",
	Short:  "Holds a private key in memory and makes it available to other processes",
	Long: `
Command v23agentd runs the security agent daemon, which holds the private key,
blessings and recognized roots of a principal in memory and makes the principal
available to other processes.

Other processes can access the agent credentials when V23_AGENT_PATH is set to
<credential dir>/agent/sock.

Exits right away if another agent is already serving the credentials.
Exits when there are no processes accessing the credentials (after a grace period).

Example:
 $ v23agentd $HOME/.credentials
 $ V23_AGENT_PATH=$HOME/.credentials/agent/sock principal dump
`,
	ArgsName: "credentials",
	ArgsLong: `
The path for the directory containing the credentials to be served by the agent.
`,
}

func runAgentD(env *cmdline.Env, args []string) error {
	if !version.Supported.Contains(versionToUse) {
		return fmt.Errorf("version %v not in the supported range %v", versionToUse, version.Supported)
	}
	if len(args) != 1 {
		return env.UsageErrorf("Expected exactly one argument: credentials")
	}
	notifyParent, detachIO, err := setupNotifyParent()
	if err != nil {
		return err
	}

	// Create principal from credentials dir.  This is safe to do outside of
	// lock since it doesn't mutate the principal.
	credentials := args[0]
	p, err := server.LoadPrincipal(credentials)
	if err != nil {
		return fmt.Errorf("failed to create new principal from dir(%s): %v", credentials, err)
	}
	cleanup, commandChannels, ipc, err := initialize(p, credentials, detachIO)
	switch err {
	case nil, errAlreadyRunning:
		notifyParent(constants.ServingMsg)
		if !detachIO {
			fmt.Printf("%v=%v\n", ref.EnvAgentPath, constants.SocketPath(credentials))
		}
		if err == errAlreadyRunning {
			return nil
		}
	default:
		return err
	}
	defer cleanup()

	noConnections := make(chan struct{})
	go idleWatch(ipc, noConnections, commandChannels)

	select {
	case sig := <-vsignals.ShutdownOnSignals(nil):
		fmt.Fprintln(os.Stderr, "Received signal", sig)
	case <-noConnections:
		fmt.Fprintln(os.Stderr, "Idle timeout")
	case <-commandChannels.exit:
		fmt.Fprintln(os.Stderr, "Received exit command")
	}
	return nil
}

var errAlreadyRunning = errors.New("already running")

// initialize sets up the service to serve the principal.  Upon success, the
// agent lock is locked and a cleanup function is returned (which includes
// unlocking the agent lock).  Otherwise, an error is returned.
func initialize(p security.Principal, credentials string, detachIO bool) (func(), commandChannels, server.IPCState, error) {
	agentDir := constants.AgentDir(credentials)
	// Lock the credentials dir and then try to grab the agent lock.  We
	// need to first lock the credentials dir before the agent lock in order
	// to avoid the following race: the old agent is about to exit; it stops
	// serving, but before it releases the agent lock, a client tries
	// connecting to the socket and fails; the client spawns a new agent
	// that comes in and tries to grab the agent lock; it can't, and then
	// exits; the old agent then also exits; this leaves no agent running.
	credsLock := lock.NewDirLock(credentials).Must()
	agentLock := lock.NewDirLock(agentDir).Must()
	credsLock.Lock()
	cleanup := credsLock.Unlock
	// In case we return early from initialize.
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		return nil, commandChannels{}, nil, err
	}
	if detachIO {
		if err := detachStdInOutErr(agentDir); err != nil {
			return nil, commandChannels{}, nil, err
		}
		// TODO(caprita): Consider ignoring SIGHUP.
	}
	if !agentLock.TryLock() {
		// Another agent is already serving the credentials.
		return nil, commandChannels{}, nil, errAlreadyRunning
	}
	cleanup = push(cleanup, agentLock.Unlock)
	ipc, err := server.Serve(p, constants.SocketPath(credentials))
	if err != nil {
		return nil, commandChannels{}, nil, fmt.Errorf("Serve failed: %v", err)
	}
	cleanup = push(cleanup, ipc.Close)
	handlers, commandCh := setupCommandHandlers(ipc)
	closeCommands, err := setupCommandSocket(agentDir, handlers)
	if err != nil {
		return nil, commandChannels{}, nil, fmt.Errorf("setupCommandSocket failed: %v", err)
	}
	cleanup = push(cleanup, func() {
		if err := closeCommands(); err != nil {
			fmt.Fprintf(os.Stderr, "closeCommands failed: %v\n", err)
		}
	})
	cleanup = push(cleanup, credsLock.Lock)
	// Disable running cleanup at the end of initialize.
	defer func() {
		cleanup = nil
	}()
	credsLock.Unlock()
	return cleanup, commandCh, ipc, nil
}

func push(a, b func()) func() {
	return func() {
		b()
		a()
	}
}

// setupNotifyParent checks if the agent was started by a parent process that
// configured a pipe over which the agent is supposed to send a status message.
// If so, it returns a function that should be called to send the status.  It
// also returns whether the agent should detach from stdout, stderr, and stdin
// following its initialization.
func setupNotifyParent() (func(string), bool, error) {
	parentPipeFD := os.Getenv("V23_AGENT_PARENT_PIPE_FD")
	if parentPipeFD == "" {
		return func(string) {}, false, nil
	}
	fd, err := strconv.Atoi(parentPipeFD)
	if err != nil {
		return nil, false, err
	}
	parentPipe := os.NewFile(uintptr(fd), "parent-pipe")
	return func(message string) {
		defer parentPipe.Close()
		if n, err := fmt.Fprintln(parentPipe, message); n != len(message)+1 || err != nil {
			// No need to stop the agent if we fail to write back to
			// the parent.  The agent is otherwise healthy.
			fmt.Fprintf(os.Stderr, "Failed to write %v to parent: (%d, %v)\n", message, n, err)
		}
	}, true, nil
}

func idleWatch(ipc server.IPCState, noConnections chan struct{}, channels commandChannels) {
	defer close(noConnections)
	grace := idleGrace
	var idleStartOverride time.Time
	idleDuration := func() time.Duration {
		idleStart := ipc.IdleStartTime()
		if idleStart.IsZero() {
			return 0
		}
		if idleStart.Before(idleStartOverride) {
			idleStart = idleStartOverride
		}
		return time.Now().Sub(idleStart)
	}
	for {
		idle := idleDuration()
		if idle > grace {
			fmt.Fprintln(os.Stderr, "IDLE for", idle, "exiting.")
			return
		}
		sleepFor := grace - idle
		if sleepFor < time.Millisecond {
			sleepFor = time.Millisecond
		}
		select {
		case <-time.After(sleepFor):
		case newGrace := <-channels.graceChange:
			grace = newGrace
		case graceReport := <-channels.graceQuery:
			graceReport <- grace
		case <-channels.idleChange:
			idleStartOverride = time.Now()
		case idleReport := <-channels.idleQuery:
			idleReport <- idleDuration()
		}
	}
}

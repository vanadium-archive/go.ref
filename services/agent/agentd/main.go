// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

// TODO(caprita): Deprecate in favor of v23agentd.

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
	"v.io/x/ref"
	vsignals "v.io/x/ref/lib/signals"
	"v.io/x/ref/services/agent/internal/lockfile"
	"v.io/x/ref/services/agent/server"
)

const pkgPath = "v.io/x/ref/services/agent/agentd"
const agentSocketName = "agent.sock"

var (
	errCantParseRestartExitCode = verror.Register(pkgPath+".errCantParseRestartExitCode", verror.NoRetry, "{1:}{2:} Failed to parse restart exit code{:_}")

	keypath, restartExitCode string
	newname, credentials     string
	withPassphrase           bool
)

func main() {
	cmdAgentD.Flags.StringVar(&keypath, "additional-principals", "", "If non-empty, allow for the creation of new principals and save them in this directory.")
	cmdAgentD.Flags.BoolVar(&withPassphrase, "with-passphrase", true, "If true, user will be prompted for principal encryption passphrase.")

	// TODO(caprita): We use the exit code of the child to determine if the
	// agent should restart it.  Consider changing this to use the unix
	// socket for this purpose.
	cmdAgentD.Flags.StringVar(&restartExitCode, "restart-exit-code", "", "If non-empty, will restart the command when it exits, provided that the command's exit code matches the value of this flag.  The value must be an integer, or an integer preceded by '!' (in which case all exit codes except the flag will trigger a restart).")

	cmdAgentD.Flags.StringVar(&newname, "new-principal-blessing-name", "", "If creating a new principal (--v23.credentials does not exist), then have it blessed with this name.")

	cmdAgentD.Flags.StringVar(&credentials, "v23.credentials", "", "The directory containing the (possibly encrypted) credentials to serve.  Must be specified.")

	cmdline.HideGlobalFlagsExcept()
	cmdline.Main(cmdAgentD)
}

var cmdAgentD = &cmdline.Command{
	Runner: cmdline.RunnerFunc(runAgentD),
	Name:   "agentd",
	Short:  "Holds a private key in memory and makes it available to other processes",
	Long: `
Command agentd runs the security agent daemon, which holds a private key in
memory and makes it available to other processes.

Loads the credentials from the specified directory into memory. Then optionally
starts a command with access to these credentials via agent protocol.

Other processes can access the agent credentials when V23_AGENT_PATH is set to
<credential dir>/agent.sock.

Example:
 $ agentd --v23.credentials=$HOME/.credentials
 $ V23_AGENT_PATH=$HOME/.credentials/agent.sock principal dump
`,
	ArgsName: "command [command_args...]",
	ArgsLong: `
The command is started as a subprocess with the given [command_args...].
`,
}

func runAgentD(env *cmdline.Env, args []string) error {
	var restartOpts restartOptions
	if err := restartOpts.parse(); err != nil {
		return env.UsageErrorf("%v", err)
	}

	if len(credentials) == 0 {
		credentials = os.Getenv(ref.EnvCredentials)
	}
	if len(credentials) == 0 {
		return env.UsageErrorf("The -credentials flag must be specified.")
	}
	p, passphrase, err := server.LoadOrCreatePrincipal(credentials, newname, withPassphrase)
	if keypath == "" && passphrase != nil {
		// If we're done with the passphrase, zero it out so it doesn't stay in memory
		for i := range passphrase {
			passphrase[i] = 0
		}
		passphrase = nil
	}
	if err != nil {
		return fmt.Errorf("failed to create new principal from dir(%s): %v", credentials, err)
	}
	socketPath := filepath.Join(credentials, agentSocketName)
	if socketPath, err = filepath.Abs(socketPath); err != nil {
		return fmt.Errorf("Abs(%s) failed: %v", socketPath, err)
	}
	socketPath = filepath.Clean(socketPath)
	if err = lockfile.CreateLockfile(socketPath); err != nil {
		return fmt.Errorf("failed to lock %q: %v", socketPath, err)
	}
	defer lockfile.RemoveLockfile(socketPath)
	// Start running our server.
	if keypath == "" {
		ipc, err := server.Serve(p, socketPath)
		if err != nil {
			return fmt.Errorf("Serve failed: %v", err)
		}
		defer ipc.Close()
	} else {
		shutdown, err := server.ServeWithKeyManager(p, keypath, passphrase, socketPath)
		if err != nil {
			return fmt.Errorf("ServeWithKeyManager failed: %v", err)
		}
		defer shutdown()
	}

	if len(args) == 0 {
		<-vsignals.ShutdownOnSignals(nil)
		return nil
	}
	if err = os.Setenv(ref.EnvAgentPath, socketPath); err != nil {
		return fmt.Errorf("setenv: %v", err)
	}
	// Clear out the environment variable before starting the child.
	if err = ref.EnvClearCredentials(); err != nil {
		return fmt.Errorf("ref.EnvClearCredentials: %v", err)
	}

	exitCode := 0
	for {
		// Run the client and wait for it to finish.
		cmd := exec.Command(flag.Args()[0], flag.Args()[1:]...)
		cmd.Stdin = env.Stdin
		cmd.Stdout = env.Stdout
		cmd.Stderr = env.Stderr

		err = cmd.Start()
		if err != nil {
			return fmt.Errorf("Error starting child: %v", err)
		}
		shutdown := make(chan struct{})
		go func() {
			select {
			case sig := <-vsignals.ShutdownOnSignals(nil):
				// TODO(caprita): Should we also relay double
				// signal to the child?  That currently just
				// force exits the current process.
				if sig == vsignals.STOP {
					sig = syscall.SIGTERM
				}
				cmd.Process.Signal(sig)
			case <-shutdown:
			}
		}()
		cmd.Wait()
		close(shutdown)
		exitCode = cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
		if !restartOpts.restart(exitCode) {
			break
		}
	}
	if exitCode != 0 {
		return cmdline.ErrExitCode(exitCode)
	}
	return nil
}

type restartOptions struct {
	enabled, unless bool
	code            int
}

func (opts *restartOptions) parse() error {
	code := restartExitCode
	if code == "" {
		return nil
	}
	opts.enabled = true
	if code[0] == '!' {
		opts.unless = true
		code = code[1:]
	}
	var err error
	if opts.code, err = strconv.Atoi(code); err != nil {
		return verror.New(errCantParseRestartExitCode, nil, err)
	}
	return nil
}

func (opts *restartOptions) restart(exitCode int) bool {
	return opts.enabled && opts.unless != (exitCode == opts.code)
}

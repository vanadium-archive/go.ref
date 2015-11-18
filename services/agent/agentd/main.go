// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"

	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
	"v.io/x/ref"
	"v.io/x/ref/internal/logger"
	vsecurity "v.io/x/ref/lib/security"
	vsignals "v.io/x/ref/lib/signals"
	"v.io/x/ref/services/agent/internal/ipc"
	"v.io/x/ref/services/agent/internal/lockfile"
	"v.io/x/ref/services/agent/internal/server"
)

const pkgPath = "v.io/x/ref/services/agent/agentd"
const agentSocketName = "agent.sock"

var (
	errCantReadPassphrase       = verror.Register(pkgPath+".errCantReadPassphrase", verror.NoRetry, "{1:}{2:} failed to read passphrase{:_}")
	errNeedPassphrase           = verror.Register(pkgPath+".errNeedPassphrase", verror.NoRetry, "{1:}{2:} Passphrase required for decrypting principal{:_}")
	errCantParseRestartExitCode = verror.Register(pkgPath+".errCantParseRestartExitCode", verror.NoRetry, "{1:}{2:} Failed to parse restart exit code{:_}")

	keypath, restartExitCode string
	newname, credentials     string
	noPassphrase             bool
)

func main() {
	cmdAgentD.Flags.StringVar(&keypath, "additional-principals", "", "If non-empty, allow for the creation of new principals and save them in this directory.")
	cmdAgentD.Flags.BoolVar(&noPassphrase, "no-passphrase", false, "If true, user will not be prompted for principal encryption passphrase.")

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
	Short:  "Holds a private key in memory and makes it available to a subprocess",
	Long: `
Command agentd runs the security agent daemon, which holds a private key in
memory and makes it available to a subprocess.

Loads the private key specified in privatekey.pem in the specified
credentials directory into memory, then starts the specified command
with access to the private key via the agent protocol instead of
directly reading from disk.
`,
	ArgsName: "command [command_args...]",
	ArgsLong: `
The command is started as a subprocess with the given [command_args...].
`,
}

func runAgentD(env *cmdline.Env, args []string) error {
	if len(args) < 1 {
		return env.UsageErrorf("Need at least one argument.")
	}
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

	p, passphrase, err := newPrincipalFromDir(credentials)
	if err != nil {
		return fmt.Errorf("failed to create new principal from dir(%s): %v", credentials, err)
	}
	socketPath := filepath.Join(credentials, agentSocketName)
	if socketPath, err = filepath.Abs(socketPath); err != nil {
		return fmt.Errorf("abs: %v", err)
	}
	socketPath = filepath.Clean(socketPath)
	if err = lockfile.CreateLockfile(socketPath); err != nil {
		return fmt.Errorf("failed to lock %q: %v", socketPath, err)
	}
	defer lockfile.RemoveLockfile(socketPath)

	if keypath == "" && passphrase != nil {
		// If we're done with the passphrase, zero it out so it doesn't stay in memory
		for i := range passphrase {
			passphrase[i] = 0
		}
		passphrase = nil
	}

	// Start running our server.
	i := ipc.NewIPC()
	defer i.Close()
	if err = server.ServeAgent(i, p); err != nil {
		return fmt.Errorf("ServeAgent: %v", err)
	}
	if keypath != "" {
		if err = server.ServeKeyManager(i, keypath, passphrase); err != nil {
			return fmt.Errorf("ServeKeyManager: %v", err)
		}
	}
	if err = os.Setenv(ref.EnvAgentPath, socketPath); err != nil {
		return fmt.Errorf("setenv: %v", err)
	}
	if err = i.Listen(socketPath); err != nil {
		return err
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

func newPrincipalFromDir(dir string) (p security.Principal, pass []byte, err error) {
	p, err = vsecurity.LoadPersistentPrincipal(dir, nil)
	if os.IsNotExist(err) {
		return handleDoesNotExist(dir)
	}
	if verror.ErrorID(err) == vsecurity.ErrBadPassphrase.ID {
		return handlePassphrase(dir)
	}
	return p, nil, err
}

func handleDoesNotExist(dir string) (security.Principal, []byte, error) {
	fmt.Println("Private key file does not exist. Creating new private key...")
	var pass []byte
	if !noPassphrase {
		var err error
		if pass, err = getPassword("Enter passphrase (entering nothing will store unencrypted): "); err != nil {
			return nil, nil, verror.New(errCantReadPassphrase, nil, err)
		}
	}
	p, err := vsecurity.CreatePersistentPrincipal(dir, pass)
	if err != nil {
		return nil, pass, err
	}
	name := newname
	if len(name) == 0 {
		name = "agent_principal"
	}
	vsecurity.InitDefaultBlessings(p, name)
	return p, pass, nil
}

func handlePassphrase(dir string) (security.Principal, []byte, error) {
	if noPassphrase {
		return nil, nil, verror.New(errNeedPassphrase, nil)
	}
	pass, err := getPassword("Private key file is encrypted. Please enter passphrase.\nEnter passphrase: ")
	if err != nil {
		return nil, nil, verror.New(errCantReadPassphrase, nil, err)
	}
	p, err := vsecurity.LoadPersistentPrincipal(dir, pass)
	return p, pass, err
}

func getPassword(prompt string) ([]byte, error) {
	if !terminal.IsTerminal(int(os.Stdin.Fd())) {
		// If the standard input is not a terminal, the password is obtained by reading a line from it.
		return readPassword()
	}
	fmt.Printf(prompt)
	stop := make(chan bool)
	defer close(stop)
	state, err := terminal.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	go catchTerminationSignals(stop, state)
	defer fmt.Printf("\n")
	return terminal.ReadPassword(int(os.Stdin.Fd()))
}

// readPassword reads form Stdin until it sees '\n' or EOF.
func readPassword() ([]byte, error) {
	var pass []byte
	var total int
	for {
		b := make([]byte, 1)
		count, err := os.Stdin.Read(b)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if err == io.EOF || b[0] == '\n' {
			return pass[:total], nil
		}
		total += count
		pass = secureAppend(pass, b)
	}
}

func secureAppend(s, t []byte) []byte {
	res := append(s, t...)
	if len(res) > cap(s) {
		// When append needs to allocate a new array, clear out the old one.
		for i := range s {
			s[i] = '0'
		}
	}
	// Clear out the second array.
	for i := range t {
		t[i] = '0'
	}
	return res
}

// catchTerminationSignals catches signals to allow us to turn terminal echo back on.
func catchTerminationSignals(stop <-chan bool, state *terminal.State) {
	var successErrno syscall.Errno
	sig := make(chan os.Signal, 4)
	// Catch the blockable termination signals.
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP)
	select {
	case <-sig:
		// Start on new line in terminal.
		fmt.Printf("\n")
		if err := terminal.Restore(int(os.Stdin.Fd()), state); err != successErrno {
			logger.Global().Errorf("Failed to restore terminal state (%v), you words may not show up when you type, enter 'stty echo' to fix this.", err)
		}
		os.Exit(-1)
	case <-stop:
		signal.Stop(sig)
	}
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

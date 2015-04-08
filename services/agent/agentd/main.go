// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"

	"v.io/v23"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/envvar"
	vsecurity "v.io/x/ref/lib/security"
	vsignals "v.io/x/ref/lib/signals"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/server"

	_ "v.io/x/ref/profiles"
)

const pkgPath = "v.io/x/ref/services/agent/agentd"

var (
	errCantReadPassphrase       = verror.Register(pkgPath+".errCantReadPassphrase", verror.NoRetry, "{1:}{2:} failed to read passphrase{:_}")
	errNeedPassphrase           = verror.Register(pkgPath+".errNeedPassphrase", verror.NoRetry, "{1:}{2:} Passphrase required for decrypting principal{:_}")
	errCantParseRestartExitCode = verror.Register(pkgPath+".errCantParseRestartExitCode", verror.NoRetry, "{1:}{2:} Failed to parse restart exit code{:_}")
)

var (
	keypath      = flag.String("additional-principals", "", "If non-empty, allow for the creation of new principals and save them in this directory.")
	noPassphrase = flag.Bool("no-passphrase", false, "If true, user will not be prompted for principal encryption passphrase.")

	// TODO(caprita): We use the exit code of the child to determine if the
	// agent should restart it.  Consider changing this to use the unix
	// socket for this purpose.
	restartExitCode = flag.String("restart-exit-code", "", "If non-empty, will restart the command when it exits, provided that the command's exit code matches the value of this flag.  The value must be an integer, or an integer preceded by '!' (in which case all exit codes except the flag will trigger a restart.")
)

func main() {
	os.Exit(Main())
}

func Main() int {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [agent options] command command_args...

Loads the private key specified in privatekey.pem in %v into memory, then
starts the specified command with access to the private key via the
agent protocol instead of directly reading from disk.

`, os.Args[0], envvar.Credentials)
		flag.PrintDefaults()
	}
	flag.Parse()
	if len(flag.Args()) < 1 {
		fmt.Fprintln(os.Stderr, "Need at least one argument.")
		flag.Usage()
		return 1
	}
	var restartOpts restartOptions
	if err := restartOpts.parse(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		return 1
	}

	// This is a bit tricky. We're trying to share the runtime's
	// v23.credentials flag.  However we need to parse it before
	// creating the runtime.  We depend on the profile's init() function
	// calling flags.CreateAndRegister(flag.CommandLine, flags.Runtime)
	// This will read the envvar.Credentials env var, then our call to
	// flag.Parse() will take any override passed on the command line.
	var dir string
	if f := flag.Lookup("v23.credentials").Value; true {
		dir = f.String()
		// Clear out the flag value to prevent v23.Init from
		// trying to load this password protected principal.
		f.Set("")
	}
	if len(dir) == 0 {
		vlog.Fatalf("The %v environment variable must be set to a directory: %q", envvar.Credentials, os.Getenv(envvar.Credentials))
	}

	p, passphrase, err := newPrincipalFromDir(dir)
	if err != nil {
		vlog.Fatalf("failed to create new principal from dir(%s): %v", dir, err)
	}

	// Clear out the environment variable before v23.Init.
	if err = envvar.ClearCredentials(); err != nil {
		vlog.Fatalf("envvar.ClearCredentials: %v", err)
	}
	ctx, shutdown := v23.Init()
	defer shutdown()

	if ctx, err = v23.SetPrincipal(ctx, p); err != nil {
		vlog.Panic("failed to set principal for ctx: %v", err)
	}

	if err = os.Setenv(agent.FdVarName, "3"); err != nil {
		vlog.Fatalf("setenv: %v", err)
	}

	if *keypath == "" && passphrase != nil {
		// If we're done with the passphrase, zero it out so it doesn't stay in memory
		for i := range passphrase {
			passphrase[i] = 0
		}
		passphrase = nil
	}

	// Start running our server.
	var sock, mgrSock *os.File
	if sock, err = server.RunAnonymousAgent(ctx, p); err != nil {
		vlog.Fatalf("RunAnonymousAgent: %v", err)
	}
	if *keypath != "" {
		if mgrSock, err = server.RunKeyManager(ctx, *keypath, passphrase); err != nil {
			vlog.Fatalf("RunKeyManager: %v", err)
		}
	}

	exitCode := 0
	for {
		// Run the client and wait for it to finish.
		cmd := exec.Command(flag.Args()[0], flag.Args()[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.ExtraFiles = []*os.File{sock}

		if mgrSock != nil {
			cmd.ExtraFiles = append(cmd.ExtraFiles, mgrSock)
		}

		err = cmd.Start()
		if err != nil {
			vlog.Fatalf("Error starting child: %v", err)
		}
		shutdown := make(chan struct{})
		go func() {
			select {
			case sig := <-vsignals.ShutdownOnSignals(ctx):
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
	// TODO(caprita): If restartOpts.enabled is false, we could close these
	// right after cmd.Start().
	sock.Close()
	mgrSock.Close()
	return exitCode
}

func newPrincipalFromDir(dir string) (security.Principal, []byte, error) {
	p, err := vsecurity.LoadPersistentPrincipal(dir, nil)
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
	if !*noPassphrase {
		var err error
		if pass, err = getPassword("Enter passphrase (entering nothing will store unencrypted): "); err != nil {
			return nil, nil, verror.New(errCantReadPassphrase, nil, err)
		}
	}
	p, err := vsecurity.CreatePersistentPrincipal(dir, pass)
	if err != nil {
		return nil, pass, err
	}
	vsecurity.InitDefaultBlessings(p, "agent_principal")
	return p, pass, nil
}

func handlePassphrase(dir string) (security.Principal, []byte, error) {
	if *noPassphrase {
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
			vlog.Errorf("Failed to restore terminal state (%v), you words may not show up when you type, enter 'stty echo' to fix this.", err)
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
	code := *restartExitCode
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

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"

	"veyron.io/veyron/veyron/lib/flags/consts"
	_ "veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/agent"
	"veyron.io/veyron/veyron/security/agent/server"

	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

var keypath = flag.String("additional_principals", "", "If non-empty, allow for the creation of new principals and save them in this directory.")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [agent options] command command_args...

Loads the private key specified in privatekey.pem in %v into memory, then
starts the specified command with access to the private key via the
agent protocol instead of directly reading from disk.

`, os.Args[0], consts.VeyronCredentials)
		flag.PrintDefaults()
	}
	flag.Parse()
	if len(flag.Args()) < 1 {
		fmt.Fprintln(os.Stderr, "Need at least one argument.")
		flag.Usage()
		os.Exit(1)
	}
	// TODO(ashankar,cnicolaou): Should flags.Parse be used instead? But that adds unnecessary
	// flags like "--veyron.namespace.root", which has no meaning for this binary.
	dir := os.Getenv(consts.VeyronCredentials)
	if len(dir) == 0 {
		vlog.Fatalf("The %v environment variable must be set to a directory", consts.VeyronCredentials)
	}

	p, passphrase, err := newPrincipalFromDir(dir)
	if err != nil {
		vlog.Fatalf("failed to create new principal from dir(%s): %v", dir, err)
	}

	runtime := rt.Init(options.RuntimePrincipal{p})
	log := runtime.Logger()

	if err = os.Setenv(agent.FdVarName, "3"); err != nil {
		log.Fatalf("setenv: %v", err)
	}
	if err = os.Setenv(consts.VeyronCredentials, ""); err != nil {
		log.Fatalf("setenv: %v", err)
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
	if sock, err = server.RunAnonymousAgent(runtime, p); err != nil {
		log.Fatalf("RunAnonymousAgent: %v", err)
	}
	if *keypath != "" {
		if mgrSock, err = server.RunKeyManager(runtime, *keypath, passphrase); err != nil {
			log.Fatalf("RunKeyManager: %v", err)
		}
	}

	// Now run the client and wait for it to finish.
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
		log.Fatalf("Error starting child: %v", err)
	}
	sock.Close()
	cmd.Wait()
	status := cmd.ProcessState.Sys().(syscall.WaitStatus)
	os.Exit(status.ExitStatus())
}

func newPrincipalFromDir(dir string) (security.Principal, []byte, error) {
	p, err := vsecurity.LoadPersistentPrincipal(dir, nil)
	if os.IsNotExist(err) {
		return handleDoesNotExist(dir)
	}
	if err == vsecurity.PassphraseErr {
		return handlePassphrase(dir)
	}
	return p, nil, err
}

func handleDoesNotExist(dir string) (security.Principal, []byte, error) {
	fmt.Println("Private key file does not exist. Creating new private key...")
	pass, err := getPassword("Enter passphrase (entering nothing will store unencrypted): ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read passphrase: %v", err)
	}
	p, err := vsecurity.CreatePersistentPrincipal(dir, pass)
	if err != nil {
		return nil, pass, err
	}
	vsecurity.InitDefaultBlessings(p, "agent_principal")
	return p, pass, nil
}

func handlePassphrase(dir string) (security.Principal, []byte, error) {
	fmt.Println("Private key file is encrypted. Please enter passphrase.")
	pass, err := getPassword("Enter passphrase: ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read passphrase: %v", err)
	}
	p, err := vsecurity.LoadPersistentPrincipal(dir, pass)
	return p, pass, err
}

func getPassword(prompt string) ([]byte, error) {
	fmt.Printf(prompt)
	stop := make(chan bool)
	defer close(stop)
	state, err := terminal.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	go catchTerminationSignals(stop, state)
	return terminal.ReadPassword(int(os.Stdin.Fd()))
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

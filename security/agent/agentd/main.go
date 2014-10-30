package main

import (
	"code.google.com/p/go.crypto/ssh/terminal"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	_ "veyron.io/veyron/veyron/profiles"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/agent"
	"veyron.io/veyron/veyron/security/agent/server"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [agent options] command command_args...

Loads the private key specified in under privatekey.pem in VEYRON_CREDENTIALS into memory, then
starts the specified command with access to the private key via the
agent protocol instead of directly reading from disk.

`, os.Args[0])
		flag.PrintDefaults()
	}
	dir := os.Getenv("VEYRON_CREDENTIALS")
	if len(dir) == 0 {
		vlog.Fatal("VEYRON_CREDENTIALS must be set to directory")
	}

	p, err := newPrincipalFromDir(dir)
	if err != nil {
		vlog.Fatalf("failed to create new principal from dir(%s): %v", dir, err)
	}

	runtime := rt.Init()
	log := runtime.Logger()

	if len(flag.Args()) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	if err = os.Setenv(agent.FdVarName, "3"); err != nil {
		log.Fatalf("setenv: %v", err)
	}
	if err = os.Setenv("VEYRON_CREDENTIALS", ""); err != nil {
		log.Fatalf("setenv: %v", err)
	}

	// Start running our server.
	var sock *os.File
	if sock, err = server.RunAnonymousAgent(runtime, p); err != nil {
		log.Fatalf("RunAgent: %v", err)
	}

	// Now run the client and wait for it to finish.
	cmd := exec.Command(flag.Args()[0], flag.Args()[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{sock}

	err = cmd.Start()
	if err != nil {
		log.Fatalf("Error starting child: %v", err)
	}
	sock.Close()
	cmd.Wait()
	status := cmd.ProcessState.Sys().(syscall.WaitStatus)
	os.Exit(status.ExitStatus())
}

func newPrincipalFromDir(dir string) (security.Principal, error) {
	p, err := vsecurity.LoadPersistentPrincipal(dir, nil)
	if os.IsNotExist(err) {
		return handleDoesNotExist(dir)
	}
	if err == vsecurity.PassphraseErr {
		return handlePassphrase(dir)
	}
	return p, err
}

func handleDoesNotExist(dir string) (security.Principal, error) {
	fmt.Println("Private key file does not exist. Creating new private key...")
	pass, err := getPassword("Enter passphrase (entering nothing will store unecrypted): ")
	if err != nil {
		return nil, fmt.Errorf("failed to read passphrase: %v", err)
	}
	p, err := vsecurity.CreatePersistentPrincipal(dir, pass)
	if err != nil {
		return nil, err
	}
	vsecurity.InitDefaultBlessings(p, "agent_principal")
	return p, nil
}

func handlePassphrase(dir string) (security.Principal, error) {
	fmt.Println("Private key file is encrypted. Please enter passphrase.")
	pass, err := getPassword("Enter passphrase: ")
	if err != nil {
		return nil, fmt.Errorf("failed to read passphrase: %v", err)
	}
	return vsecurity.LoadPersistentPrincipal(dir, pass)
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

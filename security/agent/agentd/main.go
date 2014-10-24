package main

import (
	"code.google.com/p/gopass"
	"flag"
	"fmt"
	"os"
	"os/exec"
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

Loads the private key specified in under privatekey.pem in VEYRON_AGENT into memory, then
starts the specified command with access to the private key via the
agent protocol instead of directly reading from disk.

`, os.Args[0])
		flag.PrintDefaults()
	}
	// TODO(suharshs): Switch to "VEYRON_CREDENTIALS" after agent is a principal.
	// This will be the end of the old sec model here. Also change the comment above.
	dir := os.Getenv("VEYRON_AGENT")
	if len(dir) == 0 {
		vlog.Fatal("VEYRON_AGENT must be set to directory")
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
	if err = os.Setenv("VEYRON_IDENTITY", ""); err != nil {
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
	pass, err := gopass.GetPass("Enter passphrase (entering nothing will store unecrypted): ")
	if err != nil {
		return nil, fmt.Errorf("failed to read passphrase: %v", err)
	}
	p, err := vsecurity.CreatePersistentPrincipal(dir, []byte(pass))
	return p, err
}

func handlePassphrase(dir string) (security.Principal, error) {
	fmt.Println("Private key file is encrypted. Please enter passphrase.")
	pass, err := gopass.GetPass("Enter passphrase: ")
	if err != nil {
		return nil, fmt.Errorf("failed to read passphrase: %v", err)
	}
	return vsecurity.LoadPersistentPrincipal(dir, []byte(pass))
}

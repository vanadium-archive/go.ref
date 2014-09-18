package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"veyron.io/veyron/veyron/security/agent"
	"veyron.io/veyron/veyron/security/agent/server"
	"veyron.io/veyron/veyron2/rt"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [agent options] command command_args...

Loads the identity specified in VEYRON_IDENTITY into memory, then
starts the specified command with access to the identity via the
agent protocol instead of directly reading from disk.

`, os.Args[0])
		flag.PrintDefaults()
	}
	// Load the identity specified in the environment
	runtime := rt.Init()
	log := runtime.Logger()

	if len(flag.Args()) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	var err error
	if err = os.Setenv(agent.EndpointVarName, agent.CreateAgentEndpoint(3)); err != nil {
		log.Fatalf("setenv: %v", err)
	}
	if err = os.Setenv("VEYRON_IDENTITY", ""); err != nil {
		log.Fatalf("setenv: %v", err)
	}

	// Start running our server.
	var sock *os.File
	if sock, err = server.RunAnonymousAgent(runtime, runtime.Identity()); err != nil {
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

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"syscall"
	"veyron/security/agent"
	"veyron/security/agent/server"
	"veyron2/rt"
	"veyron2/vom"
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

	// Save the public id so the client can find it.
	idfile, err := ioutil.TempFile("", "agentid")
	if err != nil {
		log.Fatalf("Unable to create identity: %v", err)
	}
	defer os.Remove(idfile.Name())
	err = vom.NewEncoder(idfile).Encode(runtime.Identity().PublicID())
	idfile.Close()
	if err != nil {
		log.Fatalf("Unable to save identity: %v", err)
	}
	if err := os.Setenv(agent.EndpointVarName, agent.CreateAgentEndpoint(3)); err != nil {
		log.Fatalf("setenv: %v", err)
	}
	if err = os.Setenv("VEYRON_IDENTITY", ""); err != nil {
		log.Fatalf("setenv: %v", err)
	}
	if err = os.Setenv(agent.BlessingVarName, idfile.Name()); err != nil {
		log.Fatalf("setenv: %v", err)
	}

	// Start running our server.
	var sock *os.File
	if sock, err = server.RunAnonymousAgent(runtime, runtime.Identity()); err != nil {
		log.Fatalf("RunAgent: %v", err)
	}

	// Now run the client and wait for it to finish.
	cmd := exec.Command(os.Args[1], os.Args[2:]...)
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

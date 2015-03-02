package agent_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"v.io/x/ref/lib/testutil/v23tests"
)

//go:generate v23 test generate

func V23TestTestPassPhraseUse(i *v23tests.T) {
	// Test passphrase handling.

	bin := i.BuildGoPkg("v.io/x/ref/security/agent/agentd")
	credentials := "VEYRON_CREDENTIALS=" + i.NewTempDir()

	// Create the passphrase
	agent := bin.WithEnv(credentials).Start("echo", "Hello")
	fmt.Fprintln(agent.Stdin(), "PASSWORD")
	agent.ReadLine() // Skip over ...creating new key... message
	agent.Expect("Hello")
	agent.ExpectEOF()
	// Use it successfully
	agent = bin.WithEnv(credentials).Start("echo", "Hello")
	fmt.Fprintln(agent.Stdin(), "PASSWORD")
	agent.Expect("Hello")
	agent.ExpectEOF()

	agent = bin.WithEnv(credentials).Start("echo", "Hello")
	fmt.Fprintln(agent.Stdin(), "BADPASSWORD")
	agent.ExpectEOF()
	stderr := bytes.Buffer{}
	err := agent.Wait(nil, &stderr)
	if err == nil {
		i.Fatalf("expected an error!")
	}
	if got, want := err.Error(), "exit status 255"; got != want {
		i.Fatalf("got %q, want %q", got, want)
	}
	if got, want := stderr.String(), "passphrase incorrect for decrypting private key"; !strings.Contains(got, want) {
		i.Fatalf("%q doesn't contain %q", got, want)
	}
}

func V23TestAllPrincipalMethods(i *v23tests.T) {
	// Test all methods of the principal interface.
	// (Errors are printed to STDERR)
	agentBin := i.BuildGoPkg("v.io/x/ref/security/agent/agentd")
	principalBin := i.BuildGoPkg("v.io/x/ref/security/agent/test_principal")

	credentials := "VEYRON_CREDENTIALS=" + i.NewTempDir()
	agent := agentBin.WithEnv(credentials).Start(principalBin.Path())
	agent.WaitOrDie(nil, os.Stderr)
}

func buildAndRunPingpongServer(i *v23tests.T, rootMTArg string) *v23tests.Binary {
	pingpongBin := i.BuildGoPkg("v.io/x/ref/security/agent/pingpong")
	pingpongServer := pingpongBin.Start("--server", rootMTArg)
	// Make sure pingpong is up and running.
	pingpongServer.ExpectRE(".*Listening at.*", -1)
	return pingpongBin
}

func V23TestAgentProcesses(i *v23tests.T) {
	// Test that the agent can correctly run one or more subproccesses.
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")

	// The agent doesn't pass NAMESPACE_ROOT or other env vars through to its
	// children, so we have to pass them specifically to the commands
	// we ask it to run.
	rootMT, _ := i.GetVar("NAMESPACE_ROOT")
	rootMTArg := "--veyron.namespace.root=" + rootMT

	agentBin := i.BuildGoPkg("v.io/x/ref/security/agent/agentd")
	testChildBin := i.BuildGoPkg("v.io/x/ref/security/agent/test_child")
	credentials := "VEYRON_CREDENTIALS=" + i.NewTempDir()

	// Test running a single app.
	pingpongBin := buildAndRunPingpongServer(i, rootMTArg)
	pingpongClient := agentBin.WithEnv(credentials).Start(pingpongBin.Path(), rootMTArg)
	// Enter a newline for an empty passphrase to the agent.
	fmt.Fprintln(pingpongClient.Stdin())
	pingpongClient.ReadLine()
	pingpongClient.ExpectRE(".*Pinging...", -1)
	pingpongClient.Expect("pong")

	// Make sure that the agent does not pass VEYRON_CREDENTIALs on to its children
	agent := agentBin.WithEnv(credentials).Start("bash", "-c", "echo", "$VEYRON_CREDENTIALS")
	fmt.Fprintln(agent.Stdin())
	all, err := agent.ReadAll()
	if err != nil {
		i.Fatal(err)
	}
	if got, want := all, "\n"; got != want {
		i.Fatalf("got %q: VEYRON_CREDENTIALS should not be set when running under the agent", got)
	}

	// Test running multiple apps connecting to the same agent, we do that using
	// the test_child helper binary which runs the pingpong server and client
	// and has them talk to each other.
	args := []string{testChildBin.Path(), "--rootmt", rootMT, "--pingpong", pingpongBin.Path()}
	testChild := agentBin.WithEnv(credentials).Start(args...)
	// Enter a newline for an empty passphrase to the agent.
	fmt.Fprintln(pingpongClient.Stdin())

	testChild.Expect("running client")
	testChild.Expect("client output")
	testChild.ExpectRE(".*Pinging...", -1)
	testChild.Expect("pong")
	testChild.WaitOrDie(nil, nil)
}

func V23TestAgentRestart(i *v23tests.T) {
	v23tests.RunRootMT(i, "--veyron.tcp.address=127.0.0.1:0")
	ns, _ := i.GetVar("NAMESPACE_ROOT")

	// The agent doesn't pass NAMESPACE_ROOT or other env vars through to its
	// children, so we have to pass them specifically to the commands
	// we ask it to run.

	rootMTArg := "--veyron.namespace.root=" + ns

	agentBin := i.BuildGoPkg("v.io/x/ref/security/agent/agentd")
	vrun := i.BuildGoPkg("v.io/x/ref/cmd/vrun")

	pingpongBin := buildAndRunPingpongServer(i, rootMTArg)
	credentials := "VEYRON_CREDENTIALS=" + i.NewTempDir()

	// This script increments a counter in $COUNTER_FILE and exits with exit code 0
	// while the counter is < 5, and 1 otherwise.
	counterFile := i.NewTempFile()

	script := fmt.Sprintf("%q %q|| exit 101\n", pingpongBin.Path(), rootMTArg)
	script += fmt.Sprintf("%q %q %q|| exit 102\n", vrun.Path(), pingpongBin.Path(), rootMTArg)
	script += fmt.Sprintf("readonly COUNT=$(expr $(<%q) + 1)\n", counterFile.Name())
	script += fmt.Sprintf("echo $COUNT > %q\n", counterFile.Name())
	script += fmt.Sprintf("[[ $COUNT -lt 5 ]]; exit $?\n")

	runscript := func(status, exit, counter string) {
		_, file, line, _ := runtime.Caller(1)
		loc := fmt.Sprintf("%s:%d", filepath.Base(file), line)
		i.Logf(exit)
		counterFile.Seek(0, 0)
		fmt.Fprintln(counterFile, "0")
		agent := agentBin.WithEnv(credentials).Start("--additional_principals", i.NewTempDir(), exit, "bash", "-c", script)
		if err := agent.Wait(nil, nil); err == nil {
			if len(status) > 0 {
				i.Fatalf("%s: expected an error %q that didn't occur", loc, status)
			}
		} else {
			if got, want := err.Error(), status; got != want {
				i.Fatalf("%s: got %q, want %q", loc, got, want)
			}
		}
		buf, err := ioutil.ReadFile(counterFile.Name())
		if err != nil {
			i.Fatal(err)
		}
		if got, want := string(buf), counter; got != want {
			i.Fatalf("%s: got %q, want %q", loc, got, want)
		}
	}
	runscript("exit status 1", "--restart_exit_code=0", "5\n")
	runscript("", "--restart_exit_code=!0", "1\n")
	runscript("exit status 1", "--restart_exit_code=!1", "5\n")
}

package rt_test

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles"
)

func init() {
	modules.RegisterChild("simpleServerProgram", "", simpleServerProgram)
	modules.RegisterChild("complexServerProgram", "", complexServerProgram)
}

type dummy struct{}

func (*dummy) Echo(ipc.ServerContext) error { return nil }

// makeServer sets up a simple dummy server.
func makeServer() ipc.Server {
	server, err := rt.R().NewServer()
	if err != nil {
		vlog.Fatalf("r.NewServer error: %s", err)
	}
	if _, err := server.Listen(profiles.LocalListenSpec); err != nil {
		vlog.Fatalf("server.Listen error: %s", err)
	}
	if err := server.Serve("", &dummy{}, nil); err != nil {
		vlog.Fatalf("server.Serve error: %s", err)
	}
	return server
}

// remoteCmdLoop listens on stdin and interprets commands sent over stdin (from
// the parent process).
func remoteCmdLoop(stdin io.Reader) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stdin)
		for scanner.Scan() {
			switch scanner.Text() {
			case "stop":
				rt.R().AppCycle().Stop()
			case "forcestop":
				fmt.Println("straight exit")
				rt.R().AppCycle().ForceStop()
			case "close":
				return
			}
		}
	}()
	return func() { <-done }
}

// complexServerProgram demonstrates the recommended way to write a more
// complex server application (with several servers, a mix of interruptible
// and blocking cleanup, and parallel and sequential cleanup execution).
// For a more typical server, see simpleServerProgram.
func complexServerProgram(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	// Initialize the runtime.  This is boilerplate.
	r := rt.Init()

	// This is part of the test setup -- we need a way to accept
	// commands from the parent process to simulate Stop and
	// RemoteStop commands that would normally be issued from
	// application code.
	go remoteCmdLoop(stdin)()

	// r.Cleanup is optional, but it's a good idea to clean up, especially
	// since it takes care of flushing the logs before exiting.
	defer r.Cleanup()

	// Create a couple servers, and start serving.
	server1 := makeServer()
	server2 := makeServer()

	// This is how to wait for a shutdown.  In this example, a shutdown
	// comes from a signal or a stop command.
	var done sync.WaitGroup
	done.Add(1)

	// This is how to configure signal handling to allow clean shutdown.
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// This is how to configure handling of stop commands to allow clean
	// shutdown.
	stopChan := make(chan string, 2)
	r.AppCycle().WaitForStop(stopChan)

	// Blocking is used to prevent the process from exiting upon receiving a
	// second signal or stop command while critical cleanup code is
	// executing.
	var blocking sync.WaitGroup
	blockingCh := make(chan struct{})

	// This is how to wait for a signal or stop command and initiate the
	// clean shutdown steps.
	go func() {
		// First signal received.
		select {
		case sig := <-sigChan:
			// If the developer wants to take different actions
			// depending on the type of signal, they can do it here.
			fmt.Fprintln(stdout, "Received signal", sig)
		case stop := <-stopChan:
			fmt.Fprintln(stdout, "Stop", stop)
		}
		// This commences the cleanup stage.
		done.Done()
		// Wait for a second signal or stop command, and force an exit,
		// but only once all blocking cleanup code (if any) has
		// completed.
		select {
		case <-sigChan:
		case <-stopChan:
		}
		<-blockingCh
		os.Exit(1)
	}()

	// This communicates to the parent test driver process in our unit test
	// that this server is ready and waiting on signals or stop commands.
	// It's purely an artifact of our test setup.
	fmt.Fprintln(stdout, "Ready")

	// Wait for shutdown.
	done.Wait()

	// Stop the servers.  In this example we stop them in goroutines to
	// parallelize the wait, but if there was a dependency between the
	// servers, the developer can simply stop them sequentially.
	var waitServerStop sync.WaitGroup
	waitServerStop.Add(2)
	go func() {
		server1.Stop()
		waitServerStop.Done()
	}()
	go func() {
		server2.Stop()
		waitServerStop.Done()
	}()
	waitServerStop.Wait()

	// This is where all cleanup code should go.  By placing it at the end,
	// we make its purpose and order of execution clear.

	// This is an example of how to mix parallel and sequential cleanup
	// steps.  Most real-world servers will likely be simpler, with either
	// just sequential or just parallel cleanup stages.

	// parallelCleanup is used to wait for all goroutines executing cleanup
	// code in parallel to finish.
	var parallelCleanup sync.WaitGroup

	// Simulate four parallel cleanup steps, two blocking and two
	// interruptible.
	parallelCleanup.Add(1)
	blocking.Add(1)
	go func() {
		fmt.Fprintln(stdout, "Parallel blocking cleanup1")
		blocking.Done()
		parallelCleanup.Done()
	}()

	parallelCleanup.Add(1)
	blocking.Add(1)
	go func() {
		fmt.Fprintln(stdout, "Parallel blocking cleanup2")
		blocking.Done()
		parallelCleanup.Done()
	}()

	parallelCleanup.Add(1)
	go func() {
		fmt.Fprintln(stdout, "Parallel interruptible cleanup1")
		parallelCleanup.Done()
	}()

	parallelCleanup.Add(1)
	go func() {
		fmt.Fprintln(stdout, "Parallel interruptible cleanup2")
		parallelCleanup.Done()
	}()

	// Simulate two sequential cleanup steps, one blocking and one
	// interruptible.
	fmt.Fprintln(stdout, "Sequential blocking cleanup")
	blocking.Wait()
	close(blockingCh)

	fmt.Fprintln(stdout, "Sequential interruptible cleanup")

	parallelCleanup.Wait()
	return nil
}

// simpleServerProgram demonstrates the recommended way to write a typical
// simple server application (with one server and a clean shutdown triggered by
// a signal or a stop command).  For an example of something more involved, see
// complexServerProgram.
func simpleServerProgram(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	// Initialize the runtime.  This is boilerplate.
	r := rt.Init()

	// This is part of the test setup -- we need a way to accept
	// commands from the parent process to simulate Stop and
	// RemoteStop commands that would normally be issued from
	// application code.
	go remoteCmdLoop(stdin)()

	// r.Cleanup is optional, but it's a good idea to clean up, especially
	// since it takes care of flushing the logs before exiting.
	//
	// We use defer to ensure this is the last thing in the program (to
	// avoid shutting down the runtime while it may still be in use), and to
	// allow it to execute even if a panic occurs down the road.
	defer r.Cleanup()

	// Create a server, and start serving.
	server := makeServer()

	// This is how to wait for a shutdown.  In this example, a shutdown
	// comes from a signal or a stop command.
	//
	// Note, if the developer wants to exit immediately upon receiving a
	// signal or stop command, they can skip this, in which case the default
	// behavior is for the process to exit.
	waiter := signals.ShutdownOnSignals()

	// This communicates to the parent test driver process in our unit test
	// that this server is ready and waiting on signals or stop commands.
	// It's purely an artifact of our test setup.
	fmt.Fprintln(stdout, "Ready")

	// Use defer for anything that should still execute even if a panic
	// occurs.
	defer fmt.Fprintln(stdout, "Deferred cleanup")

	// Wait for shutdown.
	sig := <-waiter
	// The developer could take different actions depending on the type of
	// signal.
	fmt.Fprintln(stdout, "Received signal", sig)

	// Cleanup code starts here.  Alternatively, these steps could be
	// invoked through defer, but we list them here to make the order of
	// operations obvious.

	// Stop the server.
	server.Stop()

	// Note, this will not execute in cases of forced shutdown
	// (e.g. SIGSTOP), when the process calls os.Exit (e.g. via log.Fatal),
	// or when a panic occurs.
	fmt.Fprintln(stdout, "Interruptible cleanup")

	return nil
}

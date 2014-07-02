package runtime

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"veyron2/rt"
)

// complexServerProgram demonstrates the recommended way to write a more complex
// server application (with several servers, a mix of interruptible and blocking
// cleanup, and parallel and sequential cleanup execution).  For a more typical
// server, see simpleServerProgram.
func complexServerProgram() {
	// Initialize the runtime.  This is boilerplate.
	r := rt.Init()
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
	r.WaitForStop(stopChan)

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
			fmt.Println("Received signal", sig)
		case stop := <-stopChan:
			fmt.Println("Stop", stop)
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
	fmt.Println("Ready")

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
		fmt.Println("Parallel blocking cleanup1")
		blocking.Done()
		parallelCleanup.Done()
	}()

	parallelCleanup.Add(1)
	blocking.Add(1)
	go func() {
		fmt.Println("Parallel blocking cleanup2")
		blocking.Done()
		parallelCleanup.Done()
	}()

	parallelCleanup.Add(1)
	go func() {
		fmt.Println("Parallel interruptible cleanup1")
		parallelCleanup.Done()
	}()

	parallelCleanup.Add(1)
	go func() {
		fmt.Println("Parallel interruptible cleanup2")
		parallelCleanup.Done()
	}()

	// Simulate two sequential cleanup steps, one blocking and one
	// interruptible.
	fmt.Println("Sequential blocking cleanup")
	blocking.Wait()
	close(blockingCh)

	fmt.Println("Sequential interruptible cleanup")

	parallelCleanup.Wait()
}

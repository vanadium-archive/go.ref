package runtime

import (
	"fmt"

	"veyron/lib/signals"

	"veyron2/rt"
)

// simpleServerProgram demonstrates the recommended way to write a typical
// simple server application (with one server and a clean shutdown triggered by
// a signal or a stop command).  For an example of something more involved, see
// complexServerProgram.
func simpleServerProgram() {
	// Initialize the runtime.  This is boilerplate.
	r := rt.Init()

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
	fmt.Println("Ready")

	// Use defer for anything that should still execute even if a panic
	// occurs.
	defer fmt.Println("Deferred cleanup")

	// Wait for shutdown.
	sig := <-waiter
	// The developer could take different actions depending on the type of
	// signal.
	fmt.Println("Received signal", sig)

	// Cleanup code starts here.  Alternatively, these steps could be
	// invoked through defer, but we list them here to make the order of
	// operations obvious.

	// Stop the server.
	server.Stop()

	// Note, this will not execute in cases of forced shutdown
	// (e.g. SIGSTOP), when the process calls os.Exit (e.g. via log.Fatal),
	// or when a panic occurs.
	fmt.Println("Interruptible cleanup")
}

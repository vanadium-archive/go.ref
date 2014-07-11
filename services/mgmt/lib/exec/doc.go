// Package exec implements simple process creation and rendezvous, including
// sharing a secret with, and passing arbitrary configuration to, the newly
// created process.
//
// Once a parent starts a child process it can use WaitForReady to wait
// for the child to reach its 'Ready' state. Operations are provided to wait
// for the child to terminate, and to terminate the child cleaning up any state
// associated with it.
//
// A child process uses the NewChildHandle function to complete the initial
// authentication handshake and must then call the Run() function to run a
// goroutine to handle the process rendezvous. The child must call SetReady to
// indicate that it is fully initialized and ready for whatever purpose it is
// intended to fulfill.
package exec

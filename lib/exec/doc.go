// Package exec implements process creation and rendezvous, including
// sharing a secret with, and passing arbitrary configuration to, the newly
// created process via an anoymous pipe. An anonymous pipe is used since
// it is the most secure communication channel available.
//
// Once a parent starts a child process it can use WaitForReady to wait
// for the child to reach its 'Ready' state. Operations are provided to wait
// for the child to terminate, and to terminate the child, cleaning up any state
// associated with it.
//
// A child process uses the GetChildHandle function to complete the initial
// authentication handshake. The child must call SetReady to indicate that it is
// fully initialized and ready for whatever purpose it is intended to fulfill.
// This handshake is referred as the 'exec protocol'.
package exec

// Package node contains the implementation for the veyron2/mgmt/node APIs.
//
// The node manager is a server that is expected to run on every Veyron-enabled
// node, and it handles both node management and management of the applications
// running on the node.
//
// The node manager is responsible for installing, updating, and launching
// applications.  It therefore sets up a footprint on the local filesystem, both
// to maintain its internal state, and to provide applications with their own
// private workspaces.
//
// The node manager is responsible for updating itself.  The mechanism to do so
// is implementation-dependent, though each node manager expects to be supplied
// with the file path of a symbolic link file, which the node manager will then
// update to point to the updated version of itself before terminating itself.
// The node manager should therefore be launched via this symbolic link to
// enable auto-updates.  To enable updates, in addition to the symbolic link
// path, the node manager needs to be told what its application metadata is
// (such as command-line arguments and environment variables, i.e. the
// application envelope defined in the veyron2/services/mgmt/application
// package), as well as the object name for where it can fetch an updated
// envelope, and the local filesystem path for its previous version (for
// rollbacks).
//
// Finally, the node manager needs to know its own object name, so it can pass
// that along to the applications that it starts.
//
// The impl subpackage contains the implementation of the node manager service.
//
// The config subpackage encapsulates the configuration settings that form the
// node manager service's 'contract' with its environment.
//
// The noded subpackage contains the main driver.
package node

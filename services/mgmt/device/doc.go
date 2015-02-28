// Package device contains the implementation for the veyron2/mgmt/device APIs.
//
// The device manager is a server that is expected to run on every
// Veyron-enabled device, and it handles both device management and management
// of the applications running on the device.
//
// The device manager is responsible for installing, updating, and launching
// applications.  It therefore sets up a footprint on the local filesystem, both
// to maintain its internal state, and to provide applications with their own
// private workspaces.
//
// The device manager is responsible for updating itself.  The mechanism to do
// so is implementation-dependent, though each device manager expects to be
// supplied with the file path of a symbolic link file, which the device manager
// will then update to point to the updated version of itself before terminating
// itself.  The device manager should therefore be launched via this symbolic
// link to enable auto-updates.  To enable updates, in addition to the symbolic
// link path, the device manager needs to be told what its application metadata
// is (such as command-line arguments and environment variables, i.e. the
// application envelope defined in the veyron2/services/mgmt/application
// package), as well as the object name for where it can fetch an updated
// envelope, and the local filesystem path for its previous version (for
// rollbacks).
//
// Finally, the device manager needs to know its own object name, so it can pass
// that along to the applications that it starts.
//
// The impl subpackage contains the implementation of the device manager
// service.
//
// The config subpackage encapsulates the configuration settings that form the
// device manager service's 'contract' with its environment.
//
// The deviced subpackage contains the main driver.
package device

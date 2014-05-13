package mounttable

// This file defines data types that are also defined in the idl for the
// mounttable. We live with the duplication here to avoid having to depend
// on stubs which in turn depend on the runtime etc.

// mountedServer mirrors mounttable.MountedServer
type mountedServer struct {
	// Server is the OA that's mounted.
	Server string
	// TTL is the remaining time (in seconds) before the mount entry expires.
	TTL uint32
}

// mountEntry mirrors mounttable.MountEntry
type mountEntry struct {
	// Name is the mounted name.
	Name string
	// Servers (if present) specifies the mounted names (Link is empty).
	Servers []mountedServer
}

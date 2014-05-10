package mounttable

import (
	"time"

	"veyron2/ipc"
)

// mountIntoMountTable mounts a single server into a single mount table.
func mountIntoMountTable(client ipc.Client, name, server string, ttl time.Duration) error {
	call, err := client.StartCall(name, "Mount", []interface{}{server, uint32(ttl.Seconds())}, callTimeout)
	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		return ierr
	}
	return err
}

// unmountFromMountTable removes a single mounted server from a single mount table.
func unmountFromMountTable(client ipc.Client, name, server string) error {
	call, err := client.StartCall(name, "Unmount", []interface{}{server}, callTimeout)
	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		return ierr
	}
	return err
}

func (ns *namespace) Mount(name, server string, ttl time.Duration) error {
	// Resolve to all the mount tables implementing name.
	mtServers, err := ns.ResolveToMountTable(name)
	if err != nil {
		return err
	}
	// Mount the server in all the returned mount tables.
	c := make(chan error, len(mtServers))
	for _, mt := range mtServers {
		go func() {
			c <- mountIntoMountTable(ns.rt.Client(), mt, server, ttl)
		}()
	}
	// Return error if any mounts failed, since otherwise we'll get
	// inconsistent resolutions down the road.
	var finalerr error
	for _ = range mtServers {
		if err := <-c; err != nil {
			finalerr = err
		}
	}
	return finalerr
}

func (ns *namespace) Unmount(name, server string) error {
	mts, err := ns.ResolveToMountTable(name)
	if err != nil {
		return err
	}

	// Unmount the server from all the mount tables.
	c := make(chan error, len(mts))
	for _, mt := range mts {
		go func() {
			c <- unmountFromMountTable(ns.rt.Client(), mt, server)
		}()
	}
	// Return error if any mounts failed, since otherwise we'll get
	// inconsistent resolutions down the road.
	var finalerr error
	for _ = range mts {
		if err := <-c; err != nil {
			finalerr = err
		}
	}
	return finalerr
}

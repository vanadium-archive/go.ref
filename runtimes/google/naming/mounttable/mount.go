package mounttable

import (
	"time"

	"veyron2"
)

// mountIntoMountTable mounts a single server into a single mount table.
func mountIntoMountTable(runtime veyron2.Runtime, name, server string, ttl time.Duration) error {
	client := runtime.Client()
	call, err := client.StartCall(runtime.TODOContext(), name, "Mount", []interface{}{server, uint32(ttl.Seconds())}, callTimeout)
	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		return ierr
	}
	return err
}

// unmountFromMountTable removes a single mounted server from a single mount table.
func unmountFromMountTable(runtime veyron2.Runtime, name, server string) error {
	client := runtime.Client()
	call, err := client.StartCall(runtime.TODOContext(), name, "Unmount", []interface{}{server}, callTimeout)
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
			c <- mountIntoMountTable(ns.rt, mt, server, ttl)
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
			c <- unmountFromMountTable(ns.rt, mt, server)
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

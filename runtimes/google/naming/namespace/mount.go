package namespace

import (
	"time"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/vlog"
)

// mountIntoMountTable mounts a single server into a single mount table.
func mountIntoMountTable(ctx context.T, client ipc.Client, name, server string, ttl time.Duration, flags naming.MountFlag) error {
	ctx, _ = ctx.WithTimeout(callTimeout)
	call, err := client.StartCall(ctx, name, "Mount", []interface{}{server, uint32(ttl.Seconds()), flags}, options.NoResolve(true))
	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		return ierr
	}
	return err
}

// unmountFromMountTable removes a single mounted server from a single mount table.
func unmountFromMountTable(ctx context.T, client ipc.Client, name, server string) error {
	ctx, _ = ctx.WithTimeout(callTimeout)
	call, err := client.StartCall(ctx, name, "Unmount", []interface{}{server}, options.NoResolve(true))
	if err != nil {
		return err
	}
	if ierr := call.Finish(&err); ierr != nil {
		return ierr
	}
	return err
}

func (ns *namespace) Mount(ctx context.T, name, server string, ttl time.Duration, opts ...naming.MountOpt) error {
	defer vlog.LogCall()()

	var flags naming.MountFlag
	for _, o := range opts {
		// NB: used a switch since we'll be adding more options.
		switch v := o.(type) {
		case naming.ReplaceMountOpt:
			if v {
				flags |= naming.MountFlag(naming.Replace)
			}
		case naming.ServesMountTableOpt:
			if v {
				flags |= naming.MountFlag(naming.MT)
			}
		}
	}

	// Resolve to all the mount tables implementing name.
	mtServers, err := ns.ResolveToMountTable(ctx, name)
	if err != nil {
		return err
	}
	// Mount the server in all the returned mount tables.
	c := make(chan error, len(mtServers))
	for _, mt := range mtServers {
		go func() {
			c <- mountIntoMountTable(ctx, ns.rt.Client(), mt, server, ttl, flags)
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
	vlog.VI(1).Infof("Mount(%s, %s) -> %v", name, server, finalerr)
	// Forget any previous cached information about these names.
	ns.resolutionCache.forget(mtServers)
	return finalerr
}

func (ns *namespace) Unmount(ctx context.T, name, server string) error {
	defer vlog.LogCall()()
	mts, err := ns.ResolveToMountTable(ctx, name)
	if err != nil {
		return err
	}

	// Unmount the server from all the mount tables.
	c := make(chan error, len(mts))
	for _, mt := range mts {
		go func() {
			c <- unmountFromMountTable(ctx, ns.rt.Client(), mt, server)
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
	ns.resolutionCache.forget(mts)
	return finalerr
}

package namespace

import (
	"fmt"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
)

// mountIntoMountTable mounts a single server into a single mount table.
func mountIntoMountTable(ctx *context.T, client rpc.Client, name, server string, patterns []security.BlessingPattern, ttl time.Duration, flags naming.MountFlag, id string) (s status) {
	s.id = id
	ctx, _ = context.WithTimeout(ctx, callTimeout)
	call, err := client.StartCall(ctx, name, "MountX", []interface{}{server, patterns, uint32(ttl.Seconds()), flags}, options.NoResolve{})
	s.err = err
	if err != nil {
		return
	}
	s.err = call.Finish()
	return
}

// Mount implements Namespace.Mount.
func (ns *namespace) Mount(ctx *context.T, name, server string, ttl time.Duration, opts ...naming.MountOpt) error {
	defer vlog.LogCall()()

	var flags naming.MountFlag
	var patterns []security.BlessingPattern
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
		case naming.MountedServerBlessingsOpt:
			patterns = str2pattern([]string(v))
		}
	}
	if len(patterns) == 0 {
		// No patterns explicitly provided. Take the conservative
		// approach that the server being mounted is run by this local
		// process.
		patterns = security.DefaultBlessingPatterns(v23.GetPrincipal(ctx))
		if len(patterns) == 0 {
			return fmt.Errorf("must provide a MountedServerBlessingsOpt")
		}
		vlog.VI(2).Infof("Mount(%s, %s): No MountedServerBlessingsOpt provided using %v", name, server, patterns)
	}

	client := v23.GetClient(ctx)
	// Mount the server in all the returned mount tables.
	f := func(ctx *context.T, mt, id string) status {
		return mountIntoMountTable(ctx, client, mt, server, patterns, ttl, flags, id)
	}
	err := ns.dispatch(ctx, name, f)
	vlog.VI(1).Infof("Mount(%s, %q, %v) -> %v", name, server, patterns, err)
	return err
}

// unmountFromMountTable removes a single mounted server from a single mount table.
func unmountFromMountTable(ctx *context.T, client rpc.Client, name, server string, id string) (s status) {
	s.id = id
	ctx, _ = context.WithTimeout(ctx, callTimeout)
	call, err := client.StartCall(ctx, name, "Unmount", []interface{}{server}, options.NoResolve{})
	s.err = err
	if err != nil {
		return
	}
	s.err = call.Finish()
	return
}

// Unmount implements Namespace.Unmount.
func (ns *namespace) Unmount(ctx *context.T, name, server string) error {
	defer vlog.LogCall()()
	// Unmount the server from all the mount tables.
	client := v23.GetClient(ctx)
	f := func(ctx *context.T, mt, id string) status {
		return unmountFromMountTable(ctx, client, mt, server, id)
	}
	err := ns.dispatch(ctx, name, f)
	vlog.VI(1).Infof("Unmount(%s, %s) -> %v", name, server, err)
	return err
}

// deleteFromMountTable deletes a name from a single mount table.  If there are any children
// and deleteSubtree isn't true, nothing is deleted.
func deleteFromMountTable(ctx *context.T, client rpc.Client, name string, deleteSubtree bool, id string) (s status) {
	s.id = id
	ctx, _ = context.WithTimeout(ctx, callTimeout)
	call, err := client.StartCall(ctx, name, "Delete", []interface{}{deleteSubtree}, options.NoResolve{})
	s.err = err
	if err != nil {
		return
	}
	s.err = call.Finish()
	return
}

// RDeleteemove implements Namespace.Delete.
func (ns *namespace) Delete(ctx *context.T, name string, deleteSubtree bool) error {
	defer vlog.LogCall()()
	// Remove from all the mount tables.
	client := v23.GetClient(ctx)
	f := func(ctx *context.T, mt, id string) status {
		return deleteFromMountTable(ctx, client, mt, deleteSubtree, id)
	}
	err := ns.dispatch(ctx, name, f)
	vlog.VI(1).Infof("Remove(%s, %v) -> %v", name, deleteSubtree, err)
	return err
}

func str2pattern(strs []string) (ret []security.BlessingPattern) {
	ret = make([]security.BlessingPattern, len(strs))
	for i, s := range strs {
		ret[i] = security.BlessingPattern(s)
	}
	return
}

package namespace

import (
	"fmt"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
)

// mountIntoMountTable mounts a single server into a single mount table.
func mountIntoMountTable(ctx *context.T, client ipc.Client, name, server string, patterns []security.BlessingPattern, ttl time.Duration, flags naming.MountFlag, id string) (s status) {
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

// unmountFromMountTable removes a single mounted server from a single mount table.
func unmountFromMountTable(ctx *context.T, client ipc.Client, name, server string, id string) (s status) {
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

func (ns *namespace) Mount(ctx *context.T, name, server string, ttl time.Duration, opts ...naming.MountOpt) error {
	defer vlog.LogCall()()

	var flags naming.MountFlag
	var patterns []string
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
			patterns = []string(v)
		}
	}
	if len(patterns) == 0 {
		// No patterns explicitly provided. Take the conservative
		// approach that the server being mounted is run by this local
		// process.
		p := v23.GetPrincipal(ctx)
		b := p.BlessingStore().Default()
		if b.IsZero() {
			return fmt.Errorf("must provide a MountedServerBlessingsOpt")
		}
		for str, _ := range p.BlessingsInfo(b) {
			patterns = append(patterns, str)
		}
		vlog.VI(2).Infof("Mount(%s, %s): No MountedServerBlessingsOpt provided using %v", name, server, patterns)
	}

	client := v23.GetClient(ctx)
	// Mount the server in all the returned mount tables.
	f := func(ctx *context.T, mt, id string) status {
		return mountIntoMountTable(ctx, client, mt, server, str2pattern(patterns), ttl, flags, id)
	}
	err := ns.dispatch(ctx, name, f)
	vlog.VI(1).Infof("Mount(%s, %q, %v) -> %v", name, server, patterns, err)
	return err
}

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

func str2pattern(strs []string) (ret []security.BlessingPattern) {
	ret = make([]security.BlessingPattern, len(strs))
	for i, s := range strs {
		ret[i] = security.BlessingPattern(s)
	}
	return
}

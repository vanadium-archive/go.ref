package namespace

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/services/security/access"
	"v.io/x/lib/vlog"
)

// setAccessListInMountTable sets the AccessList in a single server.
func setAccessListInMountTable(ctx *context.T, client rpc.Client, name string, acl access.Permissions, etag, id string) (s status) {
	s.id = id
	ctx, _ = context.WithTimeout(ctx, callTimeout)
	call, err := client.StartCall(ctx, name, "SetPermissions", []interface{}{acl, etag}, options.NoResolve{})
	s.err = err
	if err != nil {
		return
	}
	s.err = call.Finish()
	return
}

func (ns *namespace) SetPermissions(ctx *context.T, name string, acl access.Permissions, etag string) error {
	defer vlog.LogCall()()
	client := v23.GetClient(ctx)

	// Apply to all mount tables implementing the name.
	f := func(ctx *context.T, mt, id string) status {
		return setAccessListInMountTable(ctx, client, mt, acl, etag, id)
	}
	err := ns.dispatch(ctx, name, f)
	vlog.VI(1).Infof("SetPermissions(%s, %v, %s) -> %v", name, acl, etag, err)
	return err
}

// GetPermissions gets an AccessList from a mount table.
func (ns *namespace) GetPermissions(ctx *context.T, name string) (acl access.Permissions, etag string, err error) {
	defer vlog.LogCall()()
	client := v23.GetClient(ctx)

	// Resolve to all the mount tables implementing name.
	me, rerr := ns.ResolveToMountTable(ctx, name)
	if rerr != nil {
		err = rerr
		return
	}
	mts := me.Names()

	call, serr := ns.parallelStartCall(ctx, client, mts, "GetPermissions", []interface{}{})
	if serr != nil {
		err = serr
		return
	}
	err = call.Finish(&acl, &etag)
	return
}

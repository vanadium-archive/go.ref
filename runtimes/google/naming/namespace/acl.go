package namespace

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/options"
	"v.io/v23/services/security/access"
	"v.io/v23/vlog"
)

// setACLInMountTable sets the ACL in a single server.
func setACLInMountTable(ctx *context.T, client ipc.Client, name string, acl access.TaggedACLMap, etag, id string) (s status) {
	s.id = id
	ctx, _ = context.WithTimeout(ctx, callTimeout)
	call, err := client.StartCall(ctx, name, "SetACL", []interface{}{acl, etag}, options.NoResolve{})
	s.err = err
	if err != nil {
		return
	}
	s.err = call.Finish()
	return
}

func (ns *namespace) SetACL(ctx *context.T, name string, acl access.TaggedACLMap, etag string) error {
	defer vlog.LogCall()()
	client := v23.GetClient(ctx)

	// Apply to all mount tables implementing the name.
	f := func(ctx *context.T, mt, id string) status {
		return setACLInMountTable(ctx, client, mt, acl, etag, id)
	}
	err := ns.dispatch(ctx, name, f)
	vlog.VI(1).Infof("SetACL(%s, %v, %s) -> %v", name, acl, etag, err)
	return err
}

// GetACL gets an ACL from a mount table.
func (ns *namespace) GetACL(ctx *context.T, name string) (acl access.TaggedACLMap, etag string, err error) {
	defer vlog.LogCall()()
	client := v23.GetClient(ctx)

	// Resolve to all the mount tables implementing name.
	me, rerr := ns.ResolveToMountTable(ctx, name)
	if rerr != nil {
		err = rerr
		return
	}
	mts := me.Names()

	call, serr := ns.parallelStartCall(ctx, client, mts, "GetACL", []interface{}{})
	if serr != nil {
		err = serr
		return
	}
	err = call.Finish(&acl, &etag)
	return
}

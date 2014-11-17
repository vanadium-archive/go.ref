package ipc

import (
	"strings"
	"time"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/glob"
)

// TODO(toddw): Rename this file to "reserved.go".

// reservedInvoker returns a special invoker for reserved methods.  This invoker
// has access to the internal dispatchers, which allows it to perform special
// handling for methods like Glob and Signature.
func reservedInvoker(dispNormal, dispReserved ipc.Dispatcher) ipc.Invoker {
	methods := &reservedMethods{dispNormal: dispNormal, dispReserved: dispReserved}
	invoker := ipc.ReflectInvoker(methods)
	methods.selfInvoker = invoker
	return invoker
}

// reservedMethods is a regular server implementation object, which is passed to
// the regular ReflectInvoker in order to implement reserved methods.  The
// leading reserved "__" prefix is stripped before any methods are called.
//
// To add a new reserved method, simply add a method below, along with a
// description of the method.
type reservedMethods struct {
	dispNormal   ipc.Dispatcher
	dispReserved ipc.Dispatcher
	selfInvoker  ipc.Invoker
}

func (r *reservedMethods) Describe__() []ipc.InterfaceDesc {
	return []ipc.InterfaceDesc{{
		Name: "__Reserved",
		Doc:  `Reserved methods implemented by the IPC framework.  Each method name is prefixed with a double underscore "__".`,
		Methods: []ipc.MethodDesc{
			{
				Name:      "Glob",
				Doc:       "Glob returns all entries matching the pattern.",
				InArgs:    []ipc.ArgDesc{{Name: "pattern", Doc: ""}},
				OutStream: ipc.ArgDesc{Doc: "Streams matching entries back to the client."},
			},
			{
				Name: "MethodSignature",
				Doc:  "MethodSignature returns the signature for the given method.",
				InArgs: []ipc.ArgDesc{{
					Name: "method",
					Doc:  "Method name whose signature will be returned.",
				}},
				OutArgs: []ipc.ArgDesc{{
					Doc: "Method signature for the given method.",
				}},
			},
			{
				Name: "Signature",
				Doc:  "Signature returns all interface signatures implemented by the object.",
				OutArgs: []ipc.ArgDesc{{
					Doc: "All interface signatures implemented by the object.",
				}},
			},
		},
	}}
}

func (r *reservedMethods) Signature(ctxOrig ipc.ServerContext) ([]ipc.InterfaceSig, error) {
	// Copy the original context to shield ourselves from changes the flowServer makes.
	ctx := copyMutableContext(ctxOrig)
	ctx.M.Method = "__Signature"
	disp := r.dispNormal
	if naming.IsReserved(ctx.Suffix()) {
		disp = r.dispReserved
	}
	if disp == nil {
		return nil, verror.NoExistf("ipc: no dispatcher for %q.%s", ctx.Suffix(), ctx.Method())
	}
	obj, auth, err := disp.Lookup(ctx.Suffix(), ctx.Method())
	switch {
	case err != nil:
		return nil, err
	case obj == nil:
		return nil, verror.NoExistf("ipc: no invoker for %q.%s", ctx.Suffix(), ctx.Method())
	}
	if verr := authorize(ctx, auth); verr != nil {
		return nil, verr
	}
	sig, err := objectToInvoker(obj).Signature(ctx)
	if err != nil {
		return nil, err
	}
	// TODO(toddw): add the signatures returned from reservedInvoker.
	// TODO(toddw): filter based on authorization.
	return sig, nil
}

func (r *reservedMethods) MethodSignature(ctxOrig ipc.ServerContext, method string) (ipc.MethodSig, error) {
	// Copy the original context to shield ourselves from changes the flowServer makes.
	ctx := copyMutableContext(ctxOrig)
	ctx.M.Method = method
	// Reserved methods use our self invoker, to describe our own methods,
	if naming.IsReserved(method) {
		ctx.M.Method = naming.StripReserved(method)
		return r.selfInvoker.MethodSignature(ctx, ctx.Method())
	}
	disp := r.dispNormal
	if naming.IsReserved(ctx.Suffix()) {
		disp = r.dispReserved
	}
	if disp == nil {
		return ipc.MethodSig{}, verror.NoExistf("ipc: no such method %q", ctx.Method())
	}
	obj, auth, err := disp.Lookup(ctx.Suffix(), ctx.Method())
	switch {
	case err != nil:
		return ipc.MethodSig{}, err
	case obj == nil:
		return ipc.MethodSig{}, verror.NoExistf("ipc: no such method %q", ctx.Method())
	}
	if verr := authorize(ctx, auth); verr != nil {
		return ipc.MethodSig{}, verr
	}
	return objectToInvoker(obj).MethodSignature(ctx, ctx.Method())
}

func (r *reservedMethods) Glob(ctx *ipc.GlobContextStub, pattern string) error {
	// Copy the original call to shield ourselves from changes the flowServer makes.
	glob := globInternal{r.dispNormal, r.dispReserved, ctx.Suffix()}
	return glob.Glob(copyMutableCall(ctx), pattern)
}

// globInternal handles ALL the Glob requests received by a server and
// constructs a response from the state of internal server objects and the
// service objects.
//
// Internal objects exist only at the root of the server and have a name that
// starts with a double underscore ("__"). They are only visible in the Glob
// response if the double underscore is explicitly part of the pattern, e.g.
// "".Glob("__*/*"), or "".Glob("__debug/...").
//
// Service objects may choose to implement either VAllGlobber or
// VChildrenGlobber. VAllGlobber is more flexible, but VChildrenGlobber is
// simpler to implement and less prone to errors.
//
// If objects implement VAllGlobber, it must be able to handle recursive pattern
// for the entire namespace below the receiver object, i.e. "a/b".Glob("...")
// must return the name of all the objects under "a/b".
//
// If they implement VChildrenGlobber, it provides a list of the receiver's
// immediate children names, or a non-nil error if the receiver doesn't exist.
//
// globInternal constructs the Glob response by internally accessing the
// VAllGlobber or VChildrenGlobber interface of objects as many times as needed.
//
// Before accessing an object, globInternal ensures that the requester is
// authorized to access it. Internal objects require either security.DebugLabel
// or security.MonitoringLabel. Service objects require security.ResolveLabel.
type globInternal struct {
	dispNormal   ipc.Dispatcher
	dispReserved ipc.Dispatcher
	receiver     string
}

// The maximum depth of recursion in Glob. We only count recursion levels
// associated with a recursive glob pattern, e.g. a pattern like "..." will be
// allowed to recurse up to 10 levels, but "*/*/*/*/..." will go up to 14
// levels.
const maxRecursiveGlobDepth = 10

func (i *globInternal) Glob(call *mutableCall, pattern string) error {
	vlog.VI(3).Infof("ipc Glob: Incoming request: %q.Glob(%q)", i.receiver, pattern)
	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}
	disp := i.dispNormal
	call.M.MethodTags = []interface{}{security.ResolveLabel}
	if naming.IsReserved(i.receiver) || (i.receiver == "" && naming.IsReserved(pattern)) {
		disp = i.dispReserved
		call.M.MethodTags = []interface{}{security.DebugLabel | security.MonitoringLabel}
	}
	if disp == nil {
		return verror.NoExistf("ipc: Glob is not implemented by %q", i.receiver)
	}
	return i.globStep(call, disp, "", g, 0)
}

func (i *globInternal) globStep(call *mutableCall, disp ipc.Dispatcher, name string, g *glob.Glob, depth int) error {
	call.M.Suffix = naming.Join(i.receiver, name)
	if depth > maxRecursiveGlobDepth {
		err := verror.Internalf("ipc: Glob exceeded its recursion limit (%d): %q", maxRecursiveGlobDepth, call.Suffix())
		vlog.Error(err)
		return err
	}
	obj, auth, err := disp.Lookup(call.Suffix(), ipc.GlobMethod)
	switch {
	case err != nil:
		return err
	case obj == nil:
		return verror.NoExistf("ipc: invoker not found for %q.%s", call.Suffix(), ipc.GlobMethod)
	}

	// Verify that that requester is authorized for the current object.
	if err := authorize(call, auth); err != nil {
		return err
	}

	// If the object implements both VAllGlobber and VChildrenGlobber, we'll
	// use VAllGlobber.
	gs := objectToInvoker(obj).VGlob()
	if gs == nil || (gs.VAllGlobber == nil && gs.VChildrenGlobber == nil) {
		if g.Len() == 0 {
			call.Send(naming.VDLMountEntry{Name: name})
		}
		return nil
	}
	if gs.VAllGlobber != nil {
		vlog.VI(3).Infof("ipc Glob: %q implements VAllGlobber", call.Suffix())
		childCtx := &ipc.GlobContextStub{&localServerCall{call, name}}
		return gs.VAllGlobber.Glob(childCtx, g.String())
	}
	vlog.VI(3).Infof("ipc Glob: %q implements VChildrenGlobber", call.Suffix())
	children, err := gs.VChildrenGlobber.VGlobChildren()
	if err != nil {
		return nil
	}
	if g.Len() == 0 {
		call.Send(naming.VDLMountEntry{Name: name})
	}
	if g.Finished() {
		return nil
	}
	if g.Len() == 0 {
		// This is a recursive pattern. Make sure we don't recurse forever.
		depth++
	}
	for _, child := range children {
		if len(child) == 0 || strings.Contains(child, "/") {
			vlog.Errorf("ipc: %q.VGlobChildren() returned an invalid child name: %q", call.Suffix(), child)
			continue
		}
		if ok, _, left := g.MatchInitialSegment(child); ok {
			next := naming.Join(name, child)
			if err := i.globStep(call, disp, next, left, depth); err != nil {
				vlog.VI(1).Infof("ipc Glob: globStep(%q, %q): %v", next, left, err)
			}
		}
	}
	return nil
}

// An ipc.ServerCall that prepends a name to all the names in the streamed
// MountEntry objects.
type localServerCall struct {
	ipc.ServerCall
	basename string
}

var _ ipc.ServerCall = (*localServerCall)(nil)

func (c *localServerCall) Send(v interface{}) error {
	me, ok := v.(naming.VDLMountEntry)
	if !ok {
		return verror.BadArgf("unexpected stream type. Got %T, want MountEntry", v)
	}
	me.Name = naming.Join(c.basename, me.Name)
	return c.ServerCall.Send(me)
}

// copyMutableCall returns a new mutableCall copied from call.  Changes to the
// original call don't affect the mutable fields in the returned object.
func copyMutableCall(call ipc.ServerCall) *mutableCall {
	return &mutableCall{Stream: call, mutableContext: copyMutableContext(call)}
}

// copyMutableContext returns a new mutableContext copied from ctx.  Changes to
// the original ctx don't affect the mutable fields in the returned object.
func copyMutableContext(ctx ipc.ServerContext) *mutableContext {
	c := &mutableContext{T: ctx}
	c.M.ContextParams.Copy(ctx)
	c.M.Blessings = ctx.Blessings()
	c.M.Server = ctx.Server()
	return c
}

// mutableCall provides a mutable implementation of ipc.ServerCall, useful for
// our various special-cased reserved methods.
type mutableCall struct {
	ipc.Stream
	*mutableContext
}

// mutableContext is like mutableCall but only provides the context portion.
type mutableContext struct {
	context.T
	M struct {
		security.ContextParams
		Blessings security.Blessings
		Server    ipc.Server
	}
}

func (c *mutableContext) Timestamp() time.Time      { return c.M.Timestamp }
func (c *mutableContext) Method() string            { return c.M.Method }
func (c *mutableContext) MethodTags() []interface{} { return c.M.MethodTags }
func (c *mutableContext) Name() string              { return c.M.Suffix }
func (c *mutableContext) Suffix() string            { return c.M.Suffix }
func (c *mutableContext) Label() security.Label {
	return security.LabelFromMethodTags(c.M.MethodTags)
}
func (c *mutableContext) LocalPrincipal() security.Principal              { return c.M.LocalPrincipal }
func (c *mutableContext) LocalBlessings() security.Blessings              { return c.M.LocalBlessings }
func (c *mutableContext) RemoteBlessings() security.Blessings             { return c.M.RemoteBlessings }
func (c *mutableContext) LocalEndpoint() naming.Endpoint                  { return c.M.LocalEndpoint }
func (c *mutableContext) RemoteEndpoint() naming.Endpoint                 { return c.M.RemoteEndpoint }
func (c *mutableContext) RemoteDischarges() map[string]security.Discharge { return c.M.RemoteDischarges }
func (c *mutableContext) Blessings() security.Blessings                   { return c.M.Blessings }
func (c *mutableContext) Server() ipc.Server                              { return c.M.Server }

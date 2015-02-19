package ipc

import (
	"strings"
	"time"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/security/access"
	"v.io/core/veyron2/vdl/vdlroot/src/signature"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/glob"
)

// reservedInvoker returns a special invoker for reserved methods.  This invoker
// has access to the internal dispatchers, which allows it to perform special
// handling for methods like Glob and Signature.
func reservedInvoker(dispNormal, dispReserved ipc.Dispatcher) ipc.Invoker {
	methods := &reservedMethods{dispNormal: dispNormal, dispReserved: dispReserved}
	invoker := ipc.ReflectInvokerOrDie(methods)
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

func (r *reservedMethods) Signature(ctxOrig ipc.ServerContext) ([]signature.Interface, error) {
	// Copy the original context to shield ourselves from changes the flowServer makes.
	ctx := copyMutableContext(ctxOrig)
	ctx.M.Method = "__Signature"
	disp := r.dispNormal
	if naming.IsReserved(ctx.Suffix()) {
		disp = r.dispReserved
	}
	if disp == nil {
		return nil, ipc.NewErrUnknownSuffix(ctx.Context(), ctx.Suffix())
	}
	obj, _, err := disp.Lookup(ctx.Suffix())
	switch {
	case err != nil:
		return nil, err
	case obj == nil:
		return nil, ipc.NewErrUnknownSuffix(ctx.Context(), ctx.Suffix())
	}
	invoker, err := objectToInvoker(obj)
	if err != nil {
		return nil, err
	}
	sig, err := invoker.Signature(ctx)
	if err != nil {
		return nil, err
	}
	// Append the reserved methods.  We wait until now to add the "__" prefix to
	// each method, so that we can use the regular ReflectInvoker.Signature logic.
	rsig, err := r.selfInvoker.Signature(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rsig {
		for j := range rsig[i].Methods {
			rsig[i].Methods[j].Name = "__" + rsig[i].Methods[j].Name
		}
	}
	return signature.CleanInterfaces(append(sig, rsig...)), nil
}

func (r *reservedMethods) MethodSignature(ctxOrig ipc.ServerContext, method string) (signature.Method, error) {
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
		return signature.Method{}, ipc.NewErrUnknownMethod(ctx.Context(), ctx.Method())
	}
	obj, _, err := disp.Lookup(ctx.Suffix())
	switch {
	case err != nil:
		return signature.Method{}, err
	case obj == nil:
		return signature.Method{}, ipc.NewErrUnknownMethod(ctx.Context(), ctx.Method())
	}
	invoker, err := objectToInvoker(obj)
	if err != nil {
		return signature.Method{}, err
	}
	// TODO(toddw): Decide if we should hide the method signature if the
	// caller doesn't have access to call it.
	return invoker.MethodSignature(ctx, ctx.Method())
}

func (r *reservedMethods) Glob(ctx ipc.ServerCall, pattern string) error {
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
// Service objects may choose to implement either AllGlobber or ChildrenGlobber.
// AllGlobber is more flexible, but ChildrenGlobber is simpler to implement and
// less prone to errors.
//
// If objects implement AllGlobber, it must be able to handle recursive pattern
// for the entire namespace below the receiver object, i.e. "a/b".Glob("...")
// must return the name of all the objects under "a/b".
//
// If they implement ChildrenGlobber, it provides a list of the receiver's
// immediate children names, or a non-nil error if the receiver doesn't exist.
//
// globInternal constructs the Glob response by internally accessing the
// AllGlobber or ChildrenGlobber interface of objects as many times as needed.
//
// Before accessing an object, globInternal ensures that the requester is
// authorized to access it. Internal objects require access.Debug. Service
// objects require access.Resolve.
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
	call.M.Method = ipc.GlobMethod
	call.M.MethodTags = []interface{}{access.Resolve}
	if naming.IsReserved(i.receiver) || (i.receiver == "" && naming.IsReserved(pattern)) {
		disp = i.dispReserved
		call.M.MethodTags = []interface{}{access.Debug}
	}
	if disp == nil {
		return ipc.NewErrGlobNotImplemented(call.Context(), i.receiver)
	}

	type gState struct {
		name  string
		glob  *glob.Glob
		depth int
	}
	queue := []gState{gState{glob: g}}

	for len(queue) != 0 {
		select {
		case <-call.Done():
			// RPC timed out or was canceled.
			return nil
		default:
		}
		state := queue[0]
		queue = queue[1:]

		call.M.Suffix = naming.Join(i.receiver, state.name)
		if state.depth > maxRecursiveGlobDepth {
			vlog.Errorf("ipc Glob: exceeded recursion limit (%d): %q", maxRecursiveGlobDepth, call.Suffix())
			continue
		}
		obj, auth, err := disp.Lookup(call.Suffix())
		if err != nil {
			vlog.VI(3).Infof("ipc Glob: Lookup failed for %q: %v", call.Suffix(), err)
			continue
		}
		if obj == nil {
			vlog.VI(3).Infof("ipc Glob: object not found for %q", call.Suffix())
			continue
		}

		// Verify that that requester is authorized for the current object.
		if err := authorize(call, auth); err != nil {
			vlog.VI(3).Infof("ipc Glob: client is not authorized for %q: %v", call.Suffix(), err)
			continue
		}

		// If the object implements both AllGlobber and ChildrenGlobber, we'll
		// use AllGlobber.
		invoker, err := objectToInvoker(obj)
		if err != nil {
			vlog.VI(3).Infof("ipc Glob: object for %q cannot be converted to invoker: %v", call.Suffix(), err)
			continue
		}
		gs := invoker.Globber()
		if gs == nil || (gs.AllGlobber == nil && gs.ChildrenGlobber == nil) {
			if state.glob.Len() == 0 {
				call.Send(naming.VDLGlobReplyEntry{naming.VDLMountEntry{Name: state.name}})
			}
			continue
		}
		if gs.AllGlobber != nil {
			vlog.VI(3).Infof("ipc Glob: %q implements AllGlobber", call.Suffix())
			ch, err := gs.AllGlobber.Glob__(call, state.glob.String())
			if err != nil {
				vlog.VI(3).Infof("ipc Glob: %q.Glob(%q) failed: %v", call.Suffix(), state.glob, err)
				continue
			}
			if ch == nil {
				continue
			}
			for gr := range ch {
				switch v := gr.(type) {
				case naming.VDLGlobReplyEntry:
					v.Value.Name = naming.Join(state.name, v.Value.Name)
					call.Send(v)
				case naming.VDLGlobReplyError:
					v.Value.Name = naming.Join(state.name, v.Value.Name)
					call.Send(v)
				}
			}
			continue
		}
		vlog.VI(3).Infof("ipc Glob: %q implements ChildrenGlobber", call.Suffix())
		children, err := gs.ChildrenGlobber.GlobChildren__(call)
		// The requested object doesn't exist.
		if err != nil {
			continue
		}
		// The glob pattern matches the current object.
		if state.glob.Len() == 0 {
			call.Send(naming.VDLGlobReplyEntry{naming.VDLMountEntry{Name: state.name}})
		}
		// The current object has no children.
		if children == nil {
			continue
		}
		depth := state.depth
		// This is a recursive pattern. Make sure we don't recurse forever.
		if state.glob.Len() == 0 {
			depth++
		}
		for child := range children {
			if len(child) == 0 || strings.Contains(child, "/") {
				vlog.Errorf("ipc Glob: %q.GlobChildren__() sent an invalid child name: %q", call.Suffix(), child)
				continue
			}
			if ok, _, left := state.glob.MatchInitialSegment(child); ok {
				next := naming.Join(state.name, child)
				queue = append(queue, gState{next, left, depth})
			}
		}
	}
	return nil
}

// copyMutableCall returns a new mutableCall copied from call.  Changes to the
// original call don't affect the mutable fields in the returned object.
func copyMutableCall(call ipc.ServerCall) *mutableCall {
	return &mutableCall{Stream: call, mutableContext: copyMutableContext(call)}
}

// copyMutableContext returns a new mutableContext copied from ctx.  Changes to
// the original ctx don't affect the mutable fields in the returned object.
func copyMutableContext(ctx ipc.ServerContext) *mutableContext {
	c := &mutableContext{T: ctx.Context()}
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
	*context.T
	M struct {
		security.ContextParams
		Blessings security.Blessings
		Server    ipc.Server
	}
}

func (c *mutableContext) Context() *context.T                             { return c.T }
func (c *mutableContext) Timestamp() time.Time                            { return c.M.Timestamp }
func (c *mutableContext) Method() string                                  { return c.M.Method }
func (c *mutableContext) MethodTags() []interface{}                       { return c.M.MethodTags }
func (c *mutableContext) Name() string                                    { return c.M.Suffix }
func (c *mutableContext) Suffix() string                                  { return c.M.Suffix }
func (c *mutableContext) LocalPrincipal() security.Principal              { return c.M.LocalPrincipal }
func (c *mutableContext) LocalBlessings() security.Blessings              { return c.M.LocalBlessings }
func (c *mutableContext) RemoteBlessings() security.Blessings             { return c.M.RemoteBlessings }
func (c *mutableContext) LocalEndpoint() naming.Endpoint                  { return c.M.LocalEndpoint }
func (c *mutableContext) RemoteEndpoint() naming.Endpoint                 { return c.M.RemoteEndpoint }
func (c *mutableContext) RemoteDischarges() map[string]security.Discharge { return c.M.RemoteDischarges }
func (c *mutableContext) Blessings() security.Blessings                   { return c.M.Blessings }
func (c *mutableContext) Server() ipc.Server                              { return c.M.Server }

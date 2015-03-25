// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/rpc/reserved"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/vdl"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/glob"
)

// reservedInvoker returns a special invoker for reserved methods.  This invoker
// has access to the internal dispatchers, which allows it to perform special
// handling for methods like Glob and Signature.
func reservedInvoker(dispNormal, dispReserved rpc.Dispatcher) rpc.Invoker {
	methods := &reservedMethods{dispNormal: dispNormal, dispReserved: dispReserved}
	invoker := rpc.ReflectInvokerOrDie(methods)
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
	dispNormal   rpc.Dispatcher
	dispReserved rpc.Dispatcher
	selfInvoker  rpc.Invoker
}

func (r *reservedMethods) Describe__() []rpc.InterfaceDesc {
	return []rpc.InterfaceDesc{{
		Name: "__Reserved",
		Doc:  `Reserved methods implemented by the RPC framework.  Each method name is prefixed with a double underscore "__".`,
		Methods: []rpc.MethodDesc{
			{
				Name:      "Glob",
				Doc:       "Glob returns all entries matching the pattern.",
				InArgs:    []rpc.ArgDesc{{Name: "pattern", Doc: ""}},
				OutStream: rpc.ArgDesc{Doc: "Streams matching entries back to the client."},
			},
			{
				Name: "MethodSignature",
				Doc:  "MethodSignature returns the signature for the given method.",
				InArgs: []rpc.ArgDesc{{
					Name: "method",
					Doc:  "Method name whose signature will be returned.",
				}},
				OutArgs: []rpc.ArgDesc{{
					Doc: "Method signature for the given method.",
				}},
			},
			{
				Name: "Signature",
				Doc:  "Signature returns all interface signatures implemented by the object.",
				OutArgs: []rpc.ArgDesc{{
					Doc: "All interface signatures implemented by the object.",
				}},
			},
		},
	}}
}

func (r *reservedMethods) Signature(callOrig rpc.ServerCall) ([]signature.Interface, error) {
	// Copy the original context to shield ourselves from changes the flowServer makes.
	call := copyMutableServerCall(callOrig)
	call.M.Method = "__Signature"
	disp := r.dispNormal
	if naming.IsReserved(call.Suffix()) {
		disp = r.dispReserved
	}
	if disp == nil {
		return nil, rpc.NewErrUnknownSuffix(call.Context(), call.Suffix())
	}
	obj, _, err := disp.Lookup(call.Suffix())
	switch {
	case err != nil:
		return nil, err
	case obj == nil:
		return nil, rpc.NewErrUnknownSuffix(call.Context(), call.Suffix())
	}
	invoker, err := objectToInvoker(obj)
	if err != nil {
		return nil, err
	}
	sig, err := invoker.Signature(call)
	if err != nil {
		return nil, err
	}
	// Append the reserved methods.  We wait until now to add the "__" prefix to
	// each method, so that we can use the regular ReflectInvoker.Signature logic.
	rsig, err := r.selfInvoker.Signature(call)
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

func (r *reservedMethods) MethodSignature(callOrig rpc.ServerCall, method string) (signature.Method, error) {
	// Copy the original context to shield ourselves from changes the flowServer makes.
	call := copyMutableServerCall(callOrig)
	call.M.Method = method
	// Reserved methods use our self invoker, to describe our own methods,
	if naming.IsReserved(method) {
		call.M.Method = naming.StripReserved(method)
		return r.selfInvoker.MethodSignature(call, call.Method())
	}
	disp := r.dispNormal
	if naming.IsReserved(call.Suffix()) {
		disp = r.dispReserved
	}
	if disp == nil {
		return signature.Method{}, rpc.NewErrUnknownMethod(call.Context(), call.Method())
	}
	obj, _, err := disp.Lookup(call.Suffix())
	switch {
	case err != nil:
		return signature.Method{}, err
	case obj == nil:
		return signature.Method{}, rpc.NewErrUnknownMethod(call.Context(), call.Method())
	}
	invoker, err := objectToInvoker(obj)
	if err != nil {
		return signature.Method{}, err
	}
	// TODO(toddw): Decide if we should hide the method signature if the
	// caller doesn't have access to call it.
	return invoker.MethodSignature(call, call.Method())
}

func (r *reservedMethods) Glob(call rpc.StreamServerCall, pattern string) error {
	// Copy the original call to shield ourselves from changes the flowServer makes.
	glob := globInternal{r.dispNormal, r.dispReserved, call.Suffix()}
	return glob.Glob(copyMutableStreamServerCall(call), pattern)
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
	dispNormal   rpc.Dispatcher
	dispReserved rpc.Dispatcher
	receiver     string
}

// The maximum depth of recursion in Glob. We only count recursion levels
// associated with a recursive glob pattern, e.g. a pattern like "..." will be
// allowed to recurse up to 10 levels, but "*/*/*/*/..." will go up to 14
// levels.
const maxRecursiveGlobDepth = 10

func (i *globInternal) Glob(call *mutableStreamServerCall, pattern string) error {
	vlog.VI(3).Infof("rpc Glob: Incoming request: %q.Glob(%q)", i.receiver, pattern)
	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}
	disp := i.dispNormal
	call.M.Method = rpc.GlobMethod
	call.M.MethodTags = []*vdl.Value{vdl.ValueOf(access.Resolve)}
	if naming.IsReserved(i.receiver) || (i.receiver == "" && naming.IsReserved(pattern)) {
		disp = i.dispReserved
		call.M.MethodTags = []*vdl.Value{vdl.ValueOf(access.Debug)}
	}
	if disp == nil {
		return reserved.NewErrGlobNotImplemented(call.Context())
	}

	type gState struct {
		name  string
		glob  *glob.Glob
		depth int
	}
	queue := []gState{gState{glob: g}}

	someMatchesOmitted := false
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
			vlog.Errorf("rpc Glob: exceeded recursion limit (%d): %q", maxRecursiveGlobDepth, call.Suffix())
			call.Send(naming.GlobReplyError{
				naming.GlobError{Name: state.name, Error: reserved.NewErrGlobMaxRecursionReached(call.Context())},
			})
			continue
		}
		obj, auth, err := disp.Lookup(call.Suffix())
		if err != nil {
			vlog.VI(3).Infof("rpc Glob: Lookup failed for %q: %v", call.Suffix(), err)
			call.Send(naming.GlobReplyError{
				naming.GlobError{Name: state.name, Error: verror.Convert(verror.ErrNoExist, call.Context(), err)},
			})
			continue
		}
		if obj == nil {
			vlog.VI(3).Infof("rpc Glob: object not found for %q", call.Suffix())
			call.Send(naming.GlobReplyError{
				naming.GlobError{Name: state.name, Error: verror.New(verror.ErrNoExist, call.Context(), "nil object")},
			})
			continue
		}

		// Verify that that requester is authorized for the current object.
		if err := authorize(call, auth); err != nil {
			someMatchesOmitted = true
			vlog.VI(3).Infof("rpc Glob: client is not authorized for %q: %v", call.Suffix(), err)
			continue
		}

		// If the object implements both AllGlobber and ChildrenGlobber, we'll
		// use AllGlobber.
		invoker, err := objectToInvoker(obj)
		if err != nil {
			vlog.VI(3).Infof("rpc Glob: object for %q cannot be converted to invoker: %v", call.Suffix(), err)
			call.Send(naming.GlobReplyError{
				naming.GlobError{Name: state.name, Error: verror.Convert(verror.ErrInternal, call.Context(), err)},
			})
			continue
		}
		gs := invoker.Globber()
		if gs == nil || (gs.AllGlobber == nil && gs.ChildrenGlobber == nil) {
			if state.glob.Len() == 0 {
				call.Send(naming.GlobReplyEntry{naming.MountEntry{Name: state.name, IsLeaf: true}})
			} else {
				call.Send(naming.GlobReplyError{
					naming.GlobError{Name: state.name, Error: reserved.NewErrGlobNotImplemented(call.Context())},
				})
			}
			continue
		}
		if gs.AllGlobber != nil {
			vlog.VI(3).Infof("rpc Glob: %q implements AllGlobber", call.Suffix())
			ch, err := gs.AllGlobber.Glob__(call, state.glob.String())
			if err != nil {
				vlog.VI(3).Infof("rpc Glob: %q.Glob(%q) failed: %v", call.Suffix(), state.glob, err)
				call.Send(naming.GlobReplyError{naming.GlobError{Name: state.name, Error: verror.Convert(verror.ErrInternal, call.Context(), err)}})
				continue
			}
			if ch == nil {
				continue
			}
			for gr := range ch {
				switch v := gr.(type) {
				case naming.GlobReplyEntry:
					v.Value.Name = naming.Join(state.name, v.Value.Name)
					call.Send(v)
				case naming.GlobReplyError:
					v.Value.Name = naming.Join(state.name, v.Value.Name)
					call.Send(v)
				}
			}
			continue
		}
		vlog.VI(3).Infof("rpc Glob: %q implements ChildrenGlobber", call.Suffix())
		children, err := gs.ChildrenGlobber.GlobChildren__(call)
		// The requested object doesn't exist.
		if err != nil {
			call.Send(naming.GlobReplyError{naming.GlobError{Name: state.name, Error: verror.Convert(verror.ErrInternal, call.Context(), err)}})
			continue
		}
		// The glob pattern matches the current object.
		if state.glob.Len() == 0 {
			call.Send(naming.GlobReplyEntry{naming.MountEntry{Name: state.name}})
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
				vlog.Errorf("rpc Glob: %q.GlobChildren__() sent an invalid child name: %q", call.Suffix(), child)
				continue
			}
			if ok, _, left := state.glob.MatchInitialSegment(child); ok {
				next := naming.Join(state.name, child)
				queue = append(queue, gState{next, left, depth})
			}
		}
	}
	if someMatchesOmitted {
		call.Send(naming.GlobReplyError{naming.GlobError{Error: reserved.NewErrGlobMatchesOmitted(call.Context())}})
	}
	return nil
}

// copyMutableStreamServerCall returns a new mutableStreamServerCall copied from call.
// Changes to the original call don't affect the mutable fields in the returned object.
func copyMutableStreamServerCall(call rpc.StreamServerCall) *mutableStreamServerCall {
	return &mutableStreamServerCall{Stream: call, mutableServerCall: copyMutableServerCall(call)}
}

// copyMutableServerCall returns a new mutableServerCall copied from call. Changes to
// the original call don't affect the mutable fields in the returned object.
func copyMutableServerCall(call rpc.ServerCall) *mutableServerCall {
	c := &mutableServerCall{}
	c.T = security.SetCall(call.Context(), c)
	c.M.CallParams.Copy(call)
	c.M.GrantedBlessings = call.GrantedBlessings()
	c.M.Server = call.Server()
	return c
}

// mutableStreamServerCall provides a mutable implementation of rpc.StreamServerCall,
// useful for our various special-cased reserved methods.
type mutableStreamServerCall struct {
	rpc.Stream
	*mutableServerCall
}

// mutableServerCall is like mutableStreamServerCall but only provides the ServerCall portion.
type mutableServerCall struct {
	*context.T
	M struct {
		security.CallParams
		GrantedBlessings security.Blessings
		Server           rpc.Server
	}
}

func (c *mutableServerCall) Context() *context.T                 { return c.T }
func (c *mutableServerCall) Timestamp() time.Time                { return c.M.Timestamp }
func (c *mutableServerCall) Method() string                      { return c.M.Method }
func (c *mutableServerCall) MethodTags() []*vdl.Value            { return c.M.MethodTags }
func (c *mutableServerCall) Name() string                        { return c.M.Suffix }
func (c *mutableServerCall) Suffix() string                      { return c.M.Suffix }
func (c *mutableServerCall) LocalPrincipal() security.Principal  { return c.M.LocalPrincipal }
func (c *mutableServerCall) LocalBlessings() security.Blessings  { return c.M.LocalBlessings }
func (c *mutableServerCall) RemoteBlessings() security.Blessings { return c.M.RemoteBlessings }
func (c *mutableServerCall) LocalEndpoint() naming.Endpoint      { return c.M.LocalEndpoint }
func (c *mutableServerCall) RemoteEndpoint() naming.Endpoint     { return c.M.RemoteEndpoint }
func (c *mutableServerCall) LocalDischarges() map[string]security.Discharge {
	return c.M.LocalDischarges
}
func (c *mutableServerCall) RemoteDischarges() map[string]security.Discharge {
	return c.M.RemoteDischarges
}
func (c *mutableServerCall) GrantedBlessings() security.Blessings { return c.M.GrantedBlessings }
func (c *mutableServerCall) Server() rpc.Server                   { return c.M.Server }
func (c *mutableServerCall) VanadiumContext() *context.T          { return c.T }

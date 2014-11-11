package ipc

import (
	"strings"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/glob"
)

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
	fs       *flowServer
	receiver string
}

// The maximum depth of recursion in Glob. We only count recursion levels
// associated with a recursive glob pattern, e.g. a pattern like "..." will be
// allowed to recurse up to 10 levels, but "*/*/*/*/..." will go up to 14
// levels.
const maxRecursiveGlobDepth = 10

func (i *globInternal) Glob(call ipc.ServerCall, pattern string) error {
	vlog.VI(3).Infof("ipc Glob: Incoming request: %q.Glob(%q)", i.receiver, pattern)
	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}
	var disp ipc.Dispatcher
	if naming.IsReserved(i.receiver) || (i.receiver == "" && naming.IsReserved(pattern)) {
		disp = i.fs.reservedOpt.Dispatcher
		i.fs.tags = []interface{}{security.DebugLabel | security.MonitoringLabel}
	} else {
		disp = i.fs.disp
		i.fs.tags = []interface{}{security.ResolveLabel}
	}
	if disp == nil {
		return verror.NoExistf("ipc: Glob is not implemented by %q", i.receiver)
	}
	return i.globStep(call, disp, "", g, 0)
}

func (i *globInternal) globStep(call ipc.ServerCall, disp ipc.Dispatcher, name string, g *glob.Glob, depth int) error {
	suffix := naming.Join(i.receiver, name)
	if depth > maxRecursiveGlobDepth {
		err := verror.Internalf("ipc: Glob exceeded its recursion limit (%d): %q", maxRecursiveGlobDepth, suffix)
		vlog.Error(err)
		return err
	}
	invoker, auth, verr := lookupInvoker(disp, suffix, ipc.GlobMethod)
	if verr != nil {
		return verr
	}
	if invoker == nil {
		return verror.NoExistf("ipc: invoker not found for %q.%s", suffix, ipc.GlobMethod)
	}

	// Verify that that requester is authorized for the current object.
	i.fs.suffix = suffix
	if err := i.fs.authorize(auth); err != nil {
		return err
	}

	// If the object implements both VAllGlobber and VChildrenGlobber, we'll
	// use VAllGlobber.
	gs := invoker.VGlob()
	if gs == nil || (gs.VAllGlobber == nil && gs.VChildrenGlobber == nil) {
		if g.Len() == 0 {
			call.Send(naming.VDLMountEntry{Name: name})
		}
		return nil
	}
	if gs.VAllGlobber != nil {
		vlog.VI(3).Infof("ipc Glob: %q implements VAllGlobber", suffix)
		childCall := &localServerCall{ServerCall: call, basename: name}
		return gs.VAllGlobber.Glob(childCall, g.String())
	}
	if gs.VChildrenGlobber != nil {
		vlog.VI(3).Infof("ipc Glob: %q implements VChildrenGlobber", suffix)
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
				vlog.Errorf("ipc: %q.VGlobChildren() returned an invalid child name: %q", suffix, child)
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

	return nil // Unreachable
}

// An ipc.ServerCall that prepends a name to all the names in the streamed
// MountEntry objects.
type localServerCall struct {
	ipc.ServerCall
	basename string
}

var _ ipc.ServerCall = (*localServerCall)(nil)
var _ ipc.Stream = (*localServerCall)(nil)
var _ ipc.ServerContext = (*localServerCall)(nil)

func (c *localServerCall) Send(v interface{}) error {
	me, ok := v.(naming.VDLMountEntry)
	if !ok {
		return verror.BadArgf("unexpected stream type. Got %T, want MountEntry", v)
	}
	me.Name = naming.Join(c.basename, me.Name)
	return c.ServerCall.Send(me)
}

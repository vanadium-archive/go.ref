// Package toplevelglob implements a Glob invoker that recurses into other
// invokers.
package toplevelglob

import (
	"veyron.io/veyron/veyron/lib/glob"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/services/mounttable/types"
	"veyron.io/veyron/veyron2/verror"
)

type invoker struct {
	d      ipc.Dispatcher
	leaves []string
}

// New returns a new top-level Glob invoker. The invoker implements a Glob
// method that will match the given leaves, and recurse into leaf invokers
// on the given dispatcher.
func New(d ipc.Dispatcher, leaves []string) ipc.Invoker {
	return ipc.ReflectInvoker(&invoker{d, leaves})
}

func (i *invoker) Glob(call ipc.ServerCall, pattern string) error {
	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}
	if g.Len() == 0 {
		call.Send(types.MountEntry{Name: ""})
	}
	for _, leaf := range i.leaves {
		if ok, _, left := g.MatchInitialSegment(leaf); ok {
			if err := i.leafGlob(call, leaf, left.String()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *invoker) leafGlob(call ipc.ServerCall, leaf string, pattern string) error {
	obj, _, err := i.d.Lookup(leaf, "Glob")
	if err != nil {
		return err
	}
	// Lookup must return an invoker if it implements its own glob
	invoker, ok := obj.(ipc.Invoker)
	if !ok {
		return nil
	}
	if invoker == nil {
		return verror.BadArgf("failed to find invoker for %q", leaf)
	}
	argptrs := []interface{}{&pattern}
	leafCall := &localServerCall{call, leaf}
	results, err := invoker.Invoke("Glob", leafCall, argptrs)
	if err != nil {
		return err
	}
	if len(results) != 1 {
		return verror.BadArgf("unexpected number of results. Got %d, want 1", len(results))
	}
	res := results[0]
	if res == nil {
		return nil
	}
	err, ok = res.(error)
	if !ok {
		return verror.BadArgf("unexpected result type. Got %T, want error", res)
	}
	return err
}

// An ipc.ServerCall implementation used to Invoke methods on the invokers
// directly. Everything is the same as the original ServerCall, except the
// Stream implementation.
type localServerCall struct {
	ipc.ServerCall
	name string
}

// Re-Implement ipc.Stream
func (c *localServerCall) Recv(v interface{}) error {
	panic("Recv not implemented")
	return nil
}

func (c *localServerCall) Send(v interface{}) error {
	me, ok := v.(types.MountEntry)
	if !ok {
		return verror.BadArgf("unexpected stream type. Got %T, want MountEntry", v)
	}
	me.Name = naming.Join(c.name, me.Name)
	return c.ServerCall.Send(me)
}

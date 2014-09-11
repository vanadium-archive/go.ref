package debug

import (
	"strings"
	"time"

	"veyron/lib/glob"
	logreaderimpl "veyron/services/mgmt/logreader/impl"
	statsimpl "veyron/services/mgmt/stats/impl"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/services/mounttable/types"
	"veyron2/verror"
)

// dispatcher holds the state of the debug dispatcher.
type dispatcher struct {
	logsDir string // The root of the logs directory.
	auth    security.Authorizer
}

func NewDispatcher(logsDir string, authorizer security.Authorizer) *dispatcher {
	return &dispatcher{logsDir, authorizer}
}

func (d *dispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	if method == "Signature" {
		return NewSignatureInvoker(suffix), d.auth, nil
	}
	if len(suffix) == 0 {
		leaves := []string{"logs", "stats"}
		return ipc.ReflectInvoker(&topLevelGlobInvoker{d, leaves}), d.auth, nil
	}
	parts := strings.SplitN(suffix, "/", 2)
	if len(parts) == 2 {
		suffix = parts[1]
	} else {
		suffix = ""
	}
	switch parts[0] {
	case "logs":
		if method == "Glob" {
			return logreaderimpl.NewLogDirectoryInvoker(d.logsDir, suffix), d.auth, nil
		}
		return logreaderimpl.NewLogFileInvoker(d.logsDir, suffix), d.auth, nil
	case "stats":
		return statsimpl.NewStatsInvoker(suffix, 10*time.Second), d.auth, nil
	}
	return nil, d.auth, nil
}

type topLevelGlobInvoker struct {
	d      *dispatcher
	leaves []string
}

func (i *topLevelGlobInvoker) Glob(call ipc.ServerCall, pattern string) error {
	g, err := glob.Parse(pattern)
	if err != nil {
		return err
	}
	for _, leaf := range i.leaves {
		if ok, left := g.MatchInitialSegment(leaf); ok {
			if err := i.leafGlob(call, leaf, left.String()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *topLevelGlobInvoker) leafGlob(call ipc.ServerCall, leaf string, pattern string) error {
	invoker, _, err := i.d.Lookup(leaf, "Glob")
	if err != nil {
		return err
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
	err, ok := res.(error)
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

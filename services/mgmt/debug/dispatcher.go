package debug

import (
	"strings"
	"time"

	"veyron/lib/glob"
	logreaderimpl "veyron/services/mgmt/logreader/impl"
	statsimpl "veyron/services/mgmt/stats/impl"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
	"veyron2/services/mounttable/types"
	"veyron2/vlog"
)

// dispatcher holds the state of the debug dispatcher.
type dispatcher struct {
	rt      veyron2.Runtime
	logsDir string // The root of the logs directory.
	auth    security.Authorizer
}

func NewDispatcher(rt veyron2.Runtime, logsDir string, authorizer security.Authorizer) *dispatcher {
	return &dispatcher{rt, logsDir, authorizer}
}

func (d *dispatcher) Lookup(suffix, method string) (ipc.Invoker, security.Authorizer, error) {
	if len(suffix) == 0 {
		leaves := []string{"logs", "stats"}
		return ipc.ReflectInvoker(&topLevelGlobInvoker{d.rt, leaves}), d.auth, nil
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
	rt     veyron2.Runtime
	leaves []string
}

func (i *topLevelGlobInvoker) Glob(call ipc.ServerCall, pattern string) error {
	vlog.VI(1).Infof("Glob(%v)", pattern)
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
	addr := naming.JoinAddressName(call.LocalEndpoint().String(), leaf)
	pattern = naming.Join(addr, pattern)
	c, err := i.rt.Namespace().Glob(call, pattern)
	if err != nil {
		vlog.VI(1).Infof("Glob(%v) failed: %v", pattern, err)
		return err
	}
	for me := range c {
		_, name := naming.SplitAddressName(me.Name)
		if err := call.Send(types.MountEntry{Name: name}); err != nil {
			return err
		}
	}
	return nil
}

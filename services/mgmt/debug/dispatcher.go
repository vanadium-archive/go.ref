package debug

import (
	"strings"
	"time"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vtrace"

	logreaderimpl "veyron.io/veyron/veyron/services/mgmt/logreader/impl"
	pprofimpl "veyron.io/veyron/veyron/services/mgmt/pprof/impl"
	statsimpl "veyron.io/veyron/veyron/services/mgmt/stats/impl"
	vtraceimpl "veyron.io/veyron/veyron/services/mgmt/vtrace/impl"
)

// dispatcher holds the state of the debug dispatcher.
type dispatcher struct {
	logsDir string // The root of the logs directory.
	auth    security.Authorizer
	store   vtrace.Store
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

func NewDispatcher(logsDir string, authorizer security.Authorizer, store vtrace.Store) *dispatcher {
	return &dispatcher{logsDir, authorizer, store}
}

// The first part of the names of the objects served by this dispatcher.
var rootName = "__debug"

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if suffix == "" {
		return ipc.VChildrenGlobberInvoker(rootName), d.auth, nil
	}
	if !strings.HasPrefix(suffix, rootName) {
		return nil, nil, nil
	}
	suffix = strings.TrimPrefix(suffix, rootName)
	suffix = strings.TrimLeft(suffix, "/")

	if suffix == "" {
		return ipc.VChildrenGlobberInvoker("logs", "pprof", "stats", "vtrace"), d.auth, nil
	}
	parts := strings.SplitN(suffix, "/", 2)
	if len(parts) == 2 {
		suffix = parts[1]
	} else {
		suffix = ""
	}
	switch parts[0] {
	case "logs":
		return logreaderimpl.NewLogFileService(d.logsDir, suffix), d.auth, nil
	case "pprof":
		return pprofimpl.NewPProfService(), d.auth, nil
	case "stats":
		return statsimpl.NewStatsService(suffix, 10*time.Second), d.auth, nil
	case "vtrace":
		return vtraceimpl.NewVtraceService(d.store), d.auth, nil
	}
	return nil, d.auth, nil
}

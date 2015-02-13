package debug

import (
	"strings"
	"time"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"

	logreaderimpl "v.io/core/veyron/services/mgmt/logreader/impl"
	pprofimpl "v.io/core/veyron/services/mgmt/pprof/impl"
	statsimpl "v.io/core/veyron/services/mgmt/stats/impl"
	vtraceimpl "v.io/core/veyron/services/mgmt/vtrace/impl"
)

// dispatcher holds the state of the debug dispatcher.
type dispatcher struct {
	logsDirFunc func() string // The function returns the root of the logs directory.
	auth        security.Authorizer
}

var _ ipc.Dispatcher = (*dispatcher)(nil)

func NewDispatcher(logsDirFunc func() string, authorizer security.Authorizer) ipc.Dispatcher {
	return &dispatcher{logsDirFunc, authorizer}
}

// The first part of the names of the objects served by this dispatcher.
var rootName = "__debug"

func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if suffix == "" {
		return ipc.ChildrenGlobberInvoker(rootName), d.auth, nil
	}
	if !strings.HasPrefix(suffix, rootName) {
		return nil, nil, nil
	}
	suffix = strings.TrimPrefix(suffix, rootName)
	suffix = strings.TrimLeft(suffix, "/")

	if suffix == "" {
		return ipc.ChildrenGlobberInvoker("logs", "pprof", "stats", "vtrace"), d.auth, nil
	}
	parts := strings.SplitN(suffix, "/", 2)
	if len(parts) == 2 {
		suffix = parts[1]
	} else {
		suffix = ""
	}
	switch parts[0] {
	case "logs":
		return logreaderimpl.NewLogFileService(d.logsDirFunc(), suffix), d.auth, nil
	case "pprof":
		return pprofimpl.NewPProfService(), d.auth, nil
	case "stats":
		return statsimpl.NewStatsService(suffix, 10*time.Second), d.auth, nil
	case "vtrace":
		return vtraceimpl.NewVtraceService(), d.auth, nil
	}
	return nil, d.auth, nil
}

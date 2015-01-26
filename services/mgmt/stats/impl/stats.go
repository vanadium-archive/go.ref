// Package impl implements the Stats interface from
// veyron2/services/mgmt/stats.
package impl

import (
	"time"

	libstats "v.io/core/veyron/lib/stats"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/services/mgmt/stats"
	"v.io/core/veyron2/services/watch"
	watchtypes "v.io/core/veyron2/services/watch/types"
	"v.io/core/veyron2/vdl"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"
)

type statsService struct {
	suffix    string
	watchFreq time.Duration
}

const pkgPath = "v.io/core/veyron/services/mgmt/stats/impl"

var (
	errNoValue         = verror.Register(stats.NoValue, verror.NoRetry, "{1:}{2:} object has no value{:_}")
	errOperationFailed = verror.Register(pkgPath+".errOperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
)

// NewStatsService returns a stats server implementation. The value of watchFreq
// is used to specify the time between WatchGlob updates.
func NewStatsService(suffix string, watchFreq time.Duration) interface{} {
	return stats.StatsServer(&statsService{suffix, watchFreq})
}

// Glob__ returns the name of all objects that match pattern.
func (i *statsService) Glob__(ctx ipc.ServerContext, pattern string) (<-chan naming.VDLMountEntry, error) {
	vlog.VI(1).Infof("%v.Glob__(%q)", i.suffix, pattern)

	ch := make(chan naming.VDLMountEntry)
	go func() {
		defer close(ch)
		it := libstats.Glob(i.suffix, pattern, time.Time{}, false)
		for it.Advance() {
			ch <- naming.VDLMountEntry{Name: it.Value().Key}
		}
		if err := it.Err(); err != nil {
			vlog.VI(1).Infof("libstats.Glob(%q, %q) failed: %v", i.suffix, pattern, err)
		}
	}()
	return ch, nil
}

// WatchGlob returns the name and value of the objects that match the request,
// followed by periodic updates when values change.
func (i *statsService) WatchGlob(ctx watch.GlobWatcherWatchGlobContext, req watchtypes.GlobRequest) error {
	vlog.VI(1).Infof("%v.WatchGlob(%+v)", i.suffix, req)

	var t time.Time
Loop:
	for {
		prevTime := t
		t = time.Now()
		it := libstats.Glob(i.suffix, req.Pattern, prevTime, true)
		changes := []watchtypes.Change{}
		for it.Advance() {
			v := it.Value()
			c := watchtypes.Change{
				Name:  v.Key,
				State: watchtypes.Exists,
				Value: v.Value,
			}
			changes = append(changes, c)
		}
		if err := it.Err(); err != nil {
			if err == libstats.ErrNotFound {
				return verror.Make(verror.NoExist, ctx.Context(), i.suffix)
			}
			return verror.Make(errOperationFailed, ctx.Context(), i.suffix)
		}
		for _, change := range changes {
			if err := ctx.SendStream().Send(change); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Context().Done():
			break Loop
		case <-time.After(i.watchFreq):
		}
	}
	return nil
}

// Value returns the value of the receiver object.
func (i *statsService) Value(ctx ipc.ServerContext) (vdl.AnyRep, error) {
	vlog.VI(1).Infof("%v.Value()", i.suffix)

	v, err := libstats.Value(i.suffix)
	switch err {
	case libstats.ErrNotFound:
		return nil, verror.Make(verror.NoExist, ctx.Context(), i.suffix)
	case libstats.ErrNoValue:
		return nil, verror.Make(errNoValue, ctx.Context(), i.suffix)
	case nil:
		return v, nil
	default:
		return nil, verror.Make(errOperationFailed, ctx.Context(), i.suffix)
	}
}

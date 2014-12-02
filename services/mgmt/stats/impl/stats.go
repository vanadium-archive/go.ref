// Package impl implements the Stats interface from
// veyron2/services/mgmt/stats.
package impl

import (
	"time"

	libstats "veyron.io/veyron/veyron/lib/stats"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/services/mgmt/stats"
	"veyron.io/veyron/veyron2/services/mgmt/stats/types"
	"veyron.io/veyron/veyron2/services/watch"
	watchtypes "veyron.io/veyron/veyron2/services/watch/types"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

type statsService struct {
	suffix    string
	watchFreq time.Duration
}

var (
	errNotFound        = verror.NoExistf("object not found")
	errNoValue         = verror.Make(types.NoValue, "object has no value")
	errOperationFailed = verror.Internalf("operation failed")
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
				return errNotFound
			}
			return errOperationFailed
		}
		for _, change := range changes {
			if err := ctx.SendStream().Send(change); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			break Loop
		case <-time.After(i.watchFreq):
		}
	}
	return nil
}

// Value returns the value of the receiver object.
func (i *statsService) Value(ctx ipc.ServerContext) (vdlutil.Any, error) {
	vlog.VI(1).Infof("%v.Value()", i.suffix)

	v, err := libstats.Value(i.suffix)
	switch err {
	case libstats.ErrNotFound:
		return nil, errNotFound
	case libstats.ErrNoValue:
		return nil, errNoValue
	case nil:
		return v, nil
	default:
		return nil, errOperationFailed
	}
}

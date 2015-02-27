// Package impl implements the Stats interface from
// veyron2/services/mgmt/stats.
package impl

import (
	"reflect"
	"time"

	libstats "v.io/core/veyron/lib/stats"

	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/services/mgmt/stats"
	"v.io/v23/services/watch"
	watchtypes "v.io/v23/services/watch/types"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

type statsService struct {
	suffix    string
	watchFreq time.Duration
}

const pkgPath = "v.io/core/veyron/services/mgmt/stats/impl"

var (
	errOperationFailed = verror.Register(pkgPath+".errOperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
)

// NewStatsService returns a stats server implementation. The value of watchFreq
// is used to specify the time between WatchGlob updates.
func NewStatsService(suffix string, watchFreq time.Duration) interface{} {
	return stats.StatsServer(&statsService{suffix, watchFreq})
}

// Glob__ returns the name of all objects that match pattern.
func (i *statsService) Glob__(ctx ipc.ServerContext, pattern string) (<-chan naming.VDLGlobReply, error) {
	vlog.VI(1).Infof("%v.Glob__(%q)", i.suffix, pattern)

	ch := make(chan naming.VDLGlobReply)
	go func() {
		defer close(ch)
		it := libstats.Glob(i.suffix, pattern, time.Time{}, false)
		for it.Advance() {
			ch <- naming.VDLGlobReplyEntry{naming.VDLMountEntry{Name: it.Value().Key}}
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
				Value: vdl.ValueOf(v.Value),
			}
			changes = append(changes, c)
		}
		if err := it.Err(); err != nil {
			if err == libstats.ErrNotFound {
				return verror.New(verror.ErrNoExist, ctx.Context(), i.suffix)
			}
			return verror.New(errOperationFailed, ctx.Context(), i.suffix)
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
func (i *statsService) Value(ctx ipc.ServerContext) (*vdl.Value, error) {
	vlog.VI(1).Infof("%v.Value()", i.suffix)

	rv, err := libstats.Value(i.suffix)
	switch {
	case err == libstats.ErrNotFound:
		return nil, verror.New(verror.ErrNoExist, ctx.Context(), i.suffix)
	case err == libstats.ErrNoValue:
		return nil, stats.NewErrNoValue(ctx.Context(), i.suffix)
	case err != nil:
		return nil, verror.New(errOperationFailed, ctx.Context(), i.suffix)
	}
	vv, err := vdl.ValueFromReflect(reflect.ValueOf(rv))
	if err != nil {
		return nil, verror.New(verror.ErrInternal, ctx.Context(), i.suffix, err)
	}
	return vv, nil
}

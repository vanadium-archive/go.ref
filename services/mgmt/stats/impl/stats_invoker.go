// Package impl implements the Stats interface from
// veyron2/services/mgmt/stats.
package impl

import (
	"time"

	"veyron/lib/stats"

	"veyron2/ipc"
	"veyron2/services/mgmt/stats/types"
	mttypes "veyron2/services/mounttable/types"
	watchtypes "veyron2/services/watch/types"
	"veyron2/vdl/vdlutil"
	"veyron2/verror"
	"veyron2/vlog"
)

type statsInvoker struct {
	suffix    string
	watchFreq time.Duration
}

var (
	errNotFound        = verror.NotFoundf("object not found")
	errNoValue         = verror.Make(types.NoValue, "object has no value")
	errOperationFailed = verror.Internalf("operation failed")
)

// NewStatsInvoker returns a new Invoker. The value of watchFreq is used to
// specify the time between WatchGlob updates.
func NewStatsInvoker(suffix string, watchFreq time.Duration) ipc.Invoker {
	return ipc.ReflectInvoker(&statsInvoker{suffix, watchFreq})
}

// Glob returns the name of all objects that match pattern.
func (i *statsInvoker) Glob(call ipc.ServerCall, pattern string) error {
	vlog.VI(1).Infof("%v.Glob(%q)", i.suffix, pattern)

	it := stats.Glob(i.suffix, pattern, time.Time{}, false)
	for it.Advance() {
		call.Send(mttypes.MountEntry{Name: it.Value().Key})
	}
	if err := it.Err(); err != nil {
		if err == stats.ErrNotFound {
			return errNotFound
		}
		return errOperationFailed
	}
	return nil
}

// WatchGlob returns the name and value of the objects that match the request,
// followed by periodic updates when values change.
func (i *statsInvoker) WatchGlob(call ipc.ServerCall, req watchtypes.GlobRequest) error {
	vlog.VI(1).Infof("%v.WatchGlob(%+v)", i.suffix, req)

	var t time.Time
Loop:
	for {
		prevTime := t
		t = time.Now()
		it := stats.Glob(i.suffix, req.Pattern, prevTime, true)
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
			if err == stats.ErrNotFound {
				return errNotFound
			}
			return errOperationFailed
		}
		for _, change := range changes {
			if err := call.Send(change); err != nil {
				return err
			}
		}
		select {
		case <-call.Done():
			break Loop
		case <-time.After(i.watchFreq):
		}
	}
	return nil
}

// Value returns the value of the receiver object.
func (i *statsInvoker) Value(call ipc.ServerCall) (vdlutil.Any, error) {
	vlog.VI(1).Infof("%v.Value()", i.suffix)

	v, err := stats.Value(i.suffix)
	switch err {
	case stats.ErrNotFound:
		return nil, errNotFound
	case stats.ErrNoValue:
		return nil, errNoValue
	case nil:
		return v, nil
	default:
		return nil, errOperationFailed
	}
}

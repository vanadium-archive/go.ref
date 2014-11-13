// Package impl implements the Stats interface from
// veyron2/services/mgmt/stats.
package impl

import (
	"time"

	"veyron.io/veyron/veyron/lib/stats"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/services/mgmt/stats/types"
	watchtypes "veyron.io/veyron/veyron2/services/watch/types"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

type statsImpl struct {
	suffix    string
	watchFreq time.Duration
}

var (
	errNotFound        = verror.NoExistf("object not found")
	errNoValue         = verror.Make(types.NoValue, "object has no value")
	errOperationFailed = verror.Internalf("operation failed")
)

// NewStatsServer returns a stats server implementation. The value of watchFreq
// is used to specify the time between WatchGlob updates.
func NewStatsServer(suffix string, watchFreq time.Duration) interface{} {
	return &statsImpl{suffix, watchFreq}
}

// Glob returns the name of all objects that match pattern.
func (i *statsImpl) Glob(ctx *ipc.GlobContextStub, pattern string) error {
	vlog.VI(1).Infof("%v.Glob(%q)", i.suffix, pattern)

	it := stats.Glob(i.suffix, pattern, time.Time{}, false)
	for it.Advance() {
		ctx.SendStream().Send(naming.VDLMountEntry{Name: it.Value().Key})
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
func (i *statsImpl) WatchGlob(call ipc.ServerCall, req watchtypes.GlobRequest) error {
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
func (i *statsImpl) Value(ctx ipc.ServerContext) (vdlutil.Any, error) {
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

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package impl implements the Stats interface from
// v.io/v23/services/mgmt/stats.
package impl

import (
	"reflect"
	"time"

	libstats "v.io/x/ref/lib/stats"

	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/services/mgmt/stats"
	"v.io/v23/services/watch"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

type statsService struct {
	suffix    string
	watchFreq time.Duration
}

const pkgPath = "v.io/x/ref/services/mgmt/stats/impl"

var (
	errOperationFailed = verror.Register(pkgPath+".errOperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
)

// NewStatsService returns a stats server implementation. The value of watchFreq
// is used to specify the time between WatchGlob updates.
func NewStatsService(suffix string, watchFreq time.Duration) interface{} {
	return stats.StatsServer(&statsService{suffix, watchFreq})
}

// Glob__ returns the name of all objects that match pattern.
func (i *statsService) Glob__(call rpc.ServerCall, pattern string) (<-chan naming.GlobReply, error) {
	vlog.VI(1).Infof("%v.Glob__(%q)", i.suffix, pattern)

	ch := make(chan naming.GlobReply)
	go func() {
		defer close(ch)
		it := libstats.Glob(i.suffix, pattern, time.Time{}, false)
		for it.Advance() {
			ch <- naming.GlobReplyEntry{naming.MountEntry{Name: it.Value().Key}}
		}
		if err := it.Err(); err != nil {
			vlog.VI(1).Infof("libstats.Glob(%q, %q) failed: %v", i.suffix, pattern, err)
		}
	}()
	return ch, nil
}

// WatchGlob returns the name and value of the objects that match the request,
// followed by periodic updates when values change.
func (i *statsService) WatchGlob(call watch.GlobWatcherWatchGlobServerCall, req watch.GlobRequest) error {
	vlog.VI(1).Infof("%v.WatchGlob(%+v)", i.suffix, req)

	var t time.Time
Loop:
	for {
		prevTime := t
		t = time.Now()
		it := libstats.Glob(i.suffix, req.Pattern, prevTime, true)
		changes := []watch.Change{}
		for it.Advance() {
			v := it.Value()
			c := watch.Change{
				Name:  v.Key,
				State: watch.Exists,
				Value: vdl.ValueOf(v.Value),
			}
			changes = append(changes, c)
		}
		if err := it.Err(); err != nil {
			if err == libstats.ErrNotFound {
				return verror.New(verror.ErrNoExist, call.Context(), i.suffix)
			}
			return verror.New(errOperationFailed, call.Context(), i.suffix)
		}
		for _, change := range changes {
			if err := call.SendStream().Send(change); err != nil {
				return err
			}
		}
		select {
		case <-call.Context().Done():
			break Loop
		case <-time.After(i.watchFreq):
		}
	}
	return nil
}

// Value returns the value of the receiver object.
func (i *statsService) Value(call rpc.ServerCall) (*vdl.Value, error) {
	vlog.VI(1).Infof("%v.Value()", i.suffix)

	rv, err := libstats.Value(i.suffix)
	switch {
	case err == libstats.ErrNotFound:
		return nil, verror.New(verror.ErrNoExist, call.Context(), i.suffix)
	case err == libstats.ErrNoValue:
		return nil, stats.NewErrNoValue(call.Context(), i.suffix)
	case err != nil:
		return nil, verror.New(errOperationFailed, call.Context(), i.suffix)
	}
	vv, err := vdl.ValueFromReflect(reflect.ValueOf(rv))
	if err != nil {
		return nil, verror.New(verror.ErrInternal, call.Context(), i.suffix, err)
	}
	return vv, nil
}

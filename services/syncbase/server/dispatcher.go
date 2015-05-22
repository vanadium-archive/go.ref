// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"strings"

	wire "v.io/syncbase/v23/services/syncbase"
	pubutil "v.io/syncbase/v23/syncbase/util"
	"v.io/syncbase/x/ref/services/syncbase/server/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
)

type dispatcher struct {
	s *service
}

var _ rpc.Dispatcher = (*dispatcher)(nil)

func NewDispatcher(s *service) *dispatcher {
	return &dispatcher{s: s}
}

// TODO(sadovsky): Return a real authorizer in various places below.
func (disp *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	suffix = strings.TrimPrefix(suffix, "/")
	parts := strings.SplitN(suffix, "/", 2)

	if len(suffix) == 0 {
		return wire.ServiceServer(disp.s), nil, nil
	}

	if parts[0] == util.SyncbaseSuffix {
		return disp.s.sync, nil, nil
	}

	// Validate all key atoms up front, so that we can avoid doing so in all our
	// method implementations.
	appName := parts[0]
	if !pubutil.ValidName(appName) {
		return nil, nil, wire.NewErrInvalidName(nil, suffix)
	}

	aExists := false
	var a *app
	if aint, err := disp.s.App(nil, nil, appName); err == nil {
		a = aint.(*app) // panics on failure, as desired
		aExists = true
	} else {
		if verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
			return nil, nil, err
		} else {
			a = &app{
				name: appName,
				s:    disp.s,
			}
		}
	}

	if len(parts) == 1 {
		return wire.AppServer(a), nil, nil
	}

	// All database, table, and row methods require the app to exist. If it
	// doesn't, abort early.
	if !aExists {
		return nil, nil, verror.New(verror.ErrNoExistOrNoAccess, nil, a.name)
	}

	// Note, it's possible for the app to be deleted concurrently with downstream
	// handling of this request. Depending on the order in which things execute,
	// the client may not get an error, but in any case ultimately the store will
	// end up in a consistent state.
	return nosql.NewDispatcher(a).Lookup(parts[1])
}

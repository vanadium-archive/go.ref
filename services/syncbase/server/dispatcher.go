// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"strings"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	wire "v.io/v23/services/syncbase"
	pubutil "v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/nosql"
)

type dispatcher struct {
	s *service
}

var _ rpc.Dispatcher = (*dispatcher)(nil)

func NewDispatcher(s *service) *dispatcher {
	return &dispatcher{s: s}
}

// We always return an AllowEveryone authorizer from Lookup(), and rely on our
// RPC method implementations to perform proper authorization.
var auth security.Authorizer = security.AllowEveryone()

func (disp *dispatcher) Lookup(ctx *context.T, suffix string) (interface{}, security.Authorizer, error) {
	if len(suffix) == 0 {
		return wire.ServiceServer(disp.s), auth, nil
	}
	parts := strings.SplitN(suffix, "/", 2)

	// If the first slash-separated component of suffix is SyncbaseSuffix,
	// dispatch to the sync module.
	if parts[0] == common.SyncbaseSuffix {
		return interfaces.SyncServer(disp.s.sync), auth, nil
	}

	// Validate all name components up front, so that we can avoid doing so in all
	// our method implementations.
	escAppName := parts[0]
	appName, ok := pubutil.Unescape(escAppName)
	if !ok || !pubutil.ValidAppName(appName) {
		return nil, nil, wire.NewErrInvalidName(ctx, suffix)
	}

	appExists := false
	var a *app
	if aInt, err := disp.s.App(nil, nil, appName); err == nil {
		a = aInt.(*app) // panics on failure, as desired
		appExists = true
	} else {
		if verror.ErrorID(err) != verror.ErrNoExist.ID {
			return nil, nil, err
		} else {
			a = &app{
				name: appName,
				s:    disp.s,
			}
		}
	}

	if len(parts) == 1 {
		return wire.AppServer(a), auth, nil
	}

	// All database, table, and row methods require the app to exist. If it
	// doesn't, abort early.
	if !appExists {
		return nil, nil, verror.New(verror.ErrNoExist, ctx, a.name)
	}

	// Note, it's possible for the app to be deleted concurrently with downstream
	// handling of this request. Depending on the order in which things execute,
	// the client may not get an error, but in any case ultimately the store will
	// end up in a consistent state.
	return nosql.NewDispatcher(a).Lookup(ctx, parts[1])
}

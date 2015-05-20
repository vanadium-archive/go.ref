// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"strings"

	wire "v.io/syncbase/v23/services/syncbase"
	nosqlWire "v.io/syncbase/v23/services/syncbase/nosql"
	pubutil "v.io/syncbase/v23/syncbase/util"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

type dispatcher struct {
	a util.App
}

var _ rpc.Dispatcher = (*dispatcher)(nil)

func NewDispatcher(a util.App) *dispatcher {
	return &dispatcher{a: a}
}

func (disp *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	suffix = strings.TrimPrefix(suffix, "/")
	parts := strings.Split(suffix, "/")

	if len(parts) == 0 {
		vlog.Fatal("invalid nosql.dispatcher Lookup")
	}

	// Validate all key atoms up front, so that we can avoid doing so in all our
	// method implementations.
	for _, s := range parts {
		if !pubutil.ValidName(s) {
			return nil, nil, wire.NewErrInvalidName(nil, suffix)
		}
	}

	dExists := false
	var d *database
	if dint, err := disp.a.NoSQLDatabase(nil, nil, parts[0]); err == nil {
		d = dint.(*database) // panics on failure, as desired
		dExists = true
	} else {
		if verror.ErrorID(err) != verror.ErrNoExistOrNoAccess.ID {
			return nil, nil, err
		} else {
			d = &database{
				name: parts[0],
				a:    disp.a,
			}
		}
	}

	if len(parts) == 1 {
		return nosqlWire.DatabaseServer(d), nil, nil
	}

	// All table and row methods require the database to exist. If it doesn't,
	// abort early.
	if !dExists {
		return nil, nil, verror.New(verror.ErrNoExistOrNoAccess, nil, d.name)
	}

	// Note, it's possible for the database to be deleted concurrently with
	// downstream handling of this request. Depending on the order in which things
	// execute, the client may not get an error, but in any case ultimately the
	// store will end up in a consistent state.
	t := &table{
		name: parts[1],
		d:    d,
	}
	if len(parts) == 2 {
		return nosqlWire.TableServer(t), nil, nil
	}

	r := &row{
		key: parts[2],
		t:   t,
	}
	if len(parts) == 3 {
		return nosqlWire.RowServer(r), nil, nil
	}

	return nil, nil, verror.NewErrNoExist(nil)
}

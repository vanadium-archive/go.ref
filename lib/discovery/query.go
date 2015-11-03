// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"reflect"
	"regexp"
	"strings"

	"v.io/v23/context"
	"v.io/v23/query/engine"
	"v.io/v23/query/engine/datasource"
	"v.io/v23/vdl"
)

// matcher is the interface for a matcher to match advertisements against a query.
type matcher interface {
	// targetServiceUuid returns the uuid of the target service if the query specifies
	// only one target service; otherwise returns nil.
	//
	// TODO(jhahn): Consider to return multiple target services so that the plugins
	// can filter advertisements more efficiently if possible.
	targetServiceUuid() Uuid

	// match returns true if the matcher matches the advertisement.
	match(ctx *context.T, ad *Advertisement) bool
}

// trueMatcher matches any advertisement.
type trueMatcher struct{}

func (m trueMatcher) targetServiceUuid() Uuid               { return nil }
func (m trueMatcher) match(*context.T, *Advertisement) bool { return true }

// dDS implements a datasource for syncQL, which represents one advertisement.
type dDS struct {
	ctx  *context.T
	k    string
	v    *vdl.Value
	done bool
}

// Implements datasource.Database.
func (ds *dDS) GetContext() *context.T                    { return ds.ctx }
func (ds *dDS) GetTable(string) (datasource.Table, error) { return ds, nil }

// Implements datasource.Table.
func (ds *dDS) Scan(datasource.KeyRanges) (datasource.KeyValueStream, error) { return ds, nil }

// Implements datasource.KeyValueStream.
func (ds *dDS) Advance() bool {
	if ds.done {
		return false
	}
	ds.done = true
	return true
}

func (ds *dDS) KeyValue() (string, *vdl.Value) { return ds.k, ds.v }
func (ds *dDS) Err() error                     { return nil }
func (ds *dDS) Cancel()                        { ds.done = true }

// queryMatcher matches advertisements against the given query.
type queryMatcher struct {
	// TODO(jhahn): Use the pre-compiled query when it's ready.
	query string
}

var reInterfaceName = regexp.MustCompile(`v.InterfaceName\s*=\s*"([^"]+)"`)

func (m *queryMatcher) targetServiceUuid() Uuid {
	// TODO(jhahn): Get this from the pre-compiled query when it's ready.
	if strings.Count(m.query, "v.InterfaceName") != 1 {
		return nil
	}
	matched := reInterfaceName.FindStringSubmatch(m.query)
	if len(matched) == 2 {
		return NewServiceUUID(matched[1])
	}
	return nil
}

func (m *queryMatcher) match(ctx *context.T, ad *Advertisement) bool {
	ds := &dDS{ctx: ctx, k: string(ad.Service.InstanceUuid)}
	var err error
	ds.v, err = vdl.ValueFromReflect(reflect.ValueOf(ad.Service))
	if err != nil {
		ctx.Error(err)
		return false
	}

	_, r, err := engine.Exec(ds, m.query)
	if err != nil {
		ctx.Error(err)
		return false
	}

	// Note that the datasource has only one row and so we can know whether it is
	// matched or not just with Advance() call.
	if r.Advance() {
		r.Cancel()
		return true
	}
	if err = r.Err(); err != nil {
		ctx.Error(err)
	}
	return false
}

func newMatcher(query string) (matcher, error) {
	if len(query) == 0 {
		return trueMatcher{}, nil
	}

	query = "SELECT v FROM d WHERE " + query

	// Validate the query.
	//
	// TODO(jhahn): Pre-compile the query when it's ready.
	_, r, err := engine.Exec(&dDS{}, query)
	if err != nil {
		return nil, err
	}
	r.Cancel()

	return &queryMatcher{query: query}, nil
}

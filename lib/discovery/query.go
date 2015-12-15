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
	"v.io/v23/query/engine/public"
	"v.io/v23/query/syncql"
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
	match(ad *Advertisement) bool
}

// trueMatcher matches any advertisement.
type trueMatcher struct{}

func (m trueMatcher) targetServiceUuid() Uuid   { return nil }
func (m trueMatcher) match(*Advertisement) bool { return true }

// dDS implements a datasource for syncQL, which represents one advertisement.
type dDS struct {
	ctx  *context.T
	k    string
	v    *vdl.Value
	done bool
}

// Implements datasource.Database.
func (ds *dDS) GetContext() *context.T { return ds.ctx }
func (ds *dDS) GetTable(table string, writeAccessReq bool) (datasource.Table, error) {
	if writeAccessReq {
		return nil, syncql.NewErrNotWritable(ds.ctx, table)
	}
	return ds, nil
}

// Implements datasource.Table.
func (ds *dDS) GetIndexFields() []datasource.Index {
	return []datasource.Index{datasource.Index{FieldName: "v.InterfaceName", Kind: vdl.String}}
}
func (ds *dDS) Scan(idxRanges ...datasource.IndexRanges) (datasource.KeyValueStream, error) {
	limit, _ /*ifcName*/ := limitToSingleInterface(idxRanges[1])
	if limit {
		// TODO(jkline): Limit to single interface name: ifcName
	} else {
		// TODO(jkline): Do not limit interface names.
	}
	return ds, nil
}
func (ds *dDS) Delete(k string) (bool, error) {
	return false, syncql.NewErrOperationNotSupported(ds.ctx, "delete")
}

func limitToSingleInterface(idxRange datasource.IndexRanges) (bool, string) {
	if idxRange.NilAllowed == false && len(*idxRange.StringRanges) == 1 && len((*idxRange.StringRanges)[0].Start) > 0 {
		// If limit is equal to start plus a zero byte, a single interface name is
		// begin queried.
		targetLimit := []byte((*idxRange.StringRanges)[0].Start)
		targetLimit = append(targetLimit, 0)
		if (*idxRange.StringRanges)[0].Limit == string(targetLimit) {
			return true, (*idxRange.StringRanges)[0].Start
		}
	}
	return false, ""
}

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

func (ds *dDS) addKeyValue(k string, v *vdl.Value) {
	ds.k, ds.v = k, v
	ds.done = false
}

// queryMatcher matches advertisements against the given query.
type queryMatcher struct {
	ds    *dDS
	pstmt public.PreparedStatement

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

func (m *queryMatcher) match(ad *Advertisement) bool {
	v, err := vdl.ValueFromReflect(reflect.ValueOf(ad.Service))
	if err != nil {
		m.ds.ctx.Error(err)
		return false
	}

	m.ds.addKeyValue(ad.Service.InstanceId, v)
	_, r, err := m.pstmt.Exec()
	if err != nil {
		m.ds.ctx.Error(err)
		return false
	}

	// Note that the datasource has only one row and so we can know whether it is
	// matched or not just with Advance() call.
	if r.Advance() {
		r.Cancel()
		return true
	}
	if err = r.Err(); err != nil {
		m.ds.ctx.Error(err)
	}
	return false
}

func newMatcher(ctx *context.T, query string) (matcher, error) {
	if len(query) == 0 {
		return trueMatcher{}, nil
	}

	// Prepare the query engine.
	query = "SELECT v FROM d WHERE " + query

	ds := &dDS{ctx: ctx}
	pstmt, err := engine.Create(ds).PrepareStatement(query)
	if err != nil {
		return nil, err
	}
	// Check any semantic error such as errors from pre-executing functions
	// or evaluating some literal function arguments.
	if _, _, err = pstmt.Exec(); err != nil {
		return nil, err
	}

	return &queryMatcher{ds: ds, pstmt: pstmt, query: query}, nil
}

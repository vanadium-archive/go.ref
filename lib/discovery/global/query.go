// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package global

import (
	"reflect"

	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/query/engine"
	"v.io/v23/query/engine/datasource"
	"v.io/v23/query/engine/public"
	"v.io/v23/query/syncql"
	"v.io/v23/vdl"
)

// matcher is the interface for a matcher to match service against a query.
type matcher interface {
	// match returns true if the matcher matches the service.
	match(service *discovery.Service) bool
}

// trueMatcher matches any service.
type trueMatcher struct{}

func (m trueMatcher) match(*discovery.Service) bool { return true }

// dDS implements a datasource for syncQL, which represents one service.
type dDS struct {
	ctx  *context.T
	k    string
	v    *vdl.Value
	done bool
}

// Implements datasource.Database.
func (ds *dDS) GetContext() *context.T { return ds.ctx }
func (ds *dDS) GetTable(name string, writeAccessReq bool) (datasource.Table, error) {
	if writeAccessReq {
		return nil, syncql.NewErrNotWritable(ds.ctx, name)
	}
	return ds, nil
}

// Implements datasource.Table.
func (ds *dDS) GetIndexFields() []datasource.Index                                { return nil }
func (ds *dDS) Scan(...datasource.IndexRanges) (datasource.KeyValueStream, error) { return ds, nil }
func (ds *dDS) Delete(string) (bool, error) {
	return false, syncql.NewErrOperationNotSupported(ds.ctx, "delete")
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

// qeDS implements a datasource, which is used to extract the target 'InstanceId' from the query.
type qeDS struct {
	ctx              *context.T
	targetInstanceId string
}

func (ds *qeDS) GetContext() *context.T { return ds.ctx }
func (ds *qeDS) GetTable(name string, writeAccessReq bool) (datasource.Table, error) {
	if writeAccessReq {
		return nil, syncql.NewErrNotWritable(ds.ctx, name)
	}
	return ds, nil
}
func (ds *qeDS) GetIndexFields() []datasource.Index { return nil }

func (ds *qeDS) Scan(indices ...datasource.IndexRanges) (datasource.KeyValueStream, error) {
	index := indices[0] // 0 is for the key.
	if !index.NilAllowed && len(*index.StringRanges) == 1 {
		// If limit is equal to start plus a zero byte, a single interface name is being queried.
		strRange := (*index.StringRanges)[0]
		if len(strRange.Start) > 0 && strRange.Limit == strRange.Start+"\000" {
			ds.targetInstanceId = strRange.Start
		}
	}
	return nil, nil
}

func (ds *qeDS) Delete(string) (bool, error) {
	return false, syncql.NewErrOperationNotSupported(ds.ctx, "delete")
}

// queryMatcher matches advertisements against the given query.
type queryMatcher struct {
	ds    *dDS
	pstmt public.PreparedStatement
}

func (m *queryMatcher) match(service *discovery.Service) bool {
	v, err := vdl.ValueFromReflect(reflect.ValueOf(service))
	if err != nil {
		m.ds.ctx.Error(err)
		return false
	}

	m.ds.addKeyValue(service.InstanceId, v)
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

func newMatcher(ctx *context.T, query string) (matcher, string, error) {
	if len(query) == 0 {
		return trueMatcher{}, "", nil
	}

	query = "SELECT v FROM d WHERE " + query

	// Extract the target InstanceId and check any semantic error in the query.
	qe := &qeDS{ctx: ctx}
	_, _, err := engine.Create(qe).Exec(query)
	if err != nil {
		return nil, "", err
	}

	// Prepare the query engine.
	ds := &dDS{ctx: ctx}
	pstmt, err := engine.Create(ds).PrepareStatement(query)
	if err != nil {
		// Should not happen; just for safey.
		return nil, "", err
	}

	return &queryMatcher{ds: ds, pstmt: pstmt}, qe.targetInstanceId, nil
}

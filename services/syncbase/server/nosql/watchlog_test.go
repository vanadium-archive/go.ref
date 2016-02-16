// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"reflect"
	"testing"

	"v.io/v23"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
	"v.io/x/ref/services/syncbase/store"
	"v.io/x/ref/services/syncbase/store/memstore"
	"v.io/x/ref/services/syncbase/vclock"
	"v.io/x/ref/test"
	"v.io/x/ref/test/testutil"
)

type mockCall struct {
	security.Call
	b security.Blessings
}

func (c *mockCall) Server() rpc.Server                   { return nil }
func (c *mockCall) GrantedBlessings() security.Blessings { return c.b }
func (c *mockCall) Security() security.Call              { return c }
func (c *mockCall) LocalBlessings() security.Blessings   { return c.b }
func (c *mockCall) RemoteBlessings() security.Blessings  { return c.b }

func putOp(st store.Store, key, permKey string, permVersion []byte) watchable.OpPut {
	version, _ := watchable.GetVersion(nil, st, []byte(key))
	return watchable.OpPut{watchable.PutOp{
		Key:         []byte(key),
		Version:     version,
		PermKey:     []byte(permKey),
		PermVersion: permVersion,
	}}
}

// TestWatchLogPerms checks that the recorded prefix permissions object
// used to grant access to Put/Delete operations is correct.
func TestWatchLogPerms(t *testing.T) {
	// Prepare V23.
	ctx, shutdown := test.V23Init()
	defer shutdown()
	ctx, _ = v23.WithPrincipal(ctx, testutil.NewPrincipal("root"))
	// Mock the service, store, db, table.
	c := vclock.NewVClockForTests(nil)
	st, _ := watchable.Wrap(memstore.New(), c, &watchable.Options{
		ManagedPrefixes: []string{util.RowPrefix, util.PermsPrefix},
	})
	db := &databaseReq{database: &database{name: "d", st: st}}
	tb := &tableReq{name: "tb", d: db}
	// Mock create the table.
	perms := access.Permissions{}
	for _, tag := range access.AllTypicalTags() {
		perms.Add(security.BlessingPattern("root"), string(tag))
	}
	util.Put(ctx, st, tb.stKey(), &TableData{
		Name:  tb.name,
		Perms: perms,
	})
	util.Put(ctx, st, tb.prefixPermsKey(""), perms)
	util.Put(ctx, st, tb.permsIndexStart(""), "")
	util.Put(ctx, st, tb.permsIndexLimit(""), "")
	blessings, _ := v23.GetPrincipal(ctx).BlessingStore().Default()
	call := &mockCall{b: blessings}
	var expected []watchable.Op
	resumeMarker, _ := watchable.GetResumeMarker(st)
	// Generate Put/Delete events.
	for i := 0; i < 5; i++ {
		// Set initial prefix permissions.
		if err := tb.SetPrefixPermissions(ctx, call, 0, "foo", perms); err != nil {
			t.Fatalf("tb.SetPrefixPermissions failed: %v", err)
		}
		// Put.
		row := &rowReq{key: "foobar", t: tb}
		if err := row.Put(ctx, call, 0, []byte("value")); err != nil {
			t.Fatalf("row.Put failed: %v", err)
		}
		permVersion, _ := watchable.GetVersion(ctx, st, []byte(tb.prefixPermsKey("foo")))
		expected = append(expected, putOp(st, row.stKey(), tb.prefixPermsKey("foo"), permVersion))
		// Delete.
		if err := row.Delete(ctx, call, 0); err != nil {
			t.Fatalf("row.Delete failed: %v", err)
		}
		deleteOp := watchable.OpDelete{watchable.DeleteOp{
			Key:         []byte(row.stKey()),
			PermKey:     []byte(tb.prefixPermsKey("foo")),
			PermVersion: permVersion,
		}}
		expected = append(expected, deleteOp)
		// DeleteRange.
		if err := row.Put(ctx, call, 0, []byte("value")); err != nil {
			t.Fatalf("row.Put failed: %v", err)
		}
		if err := tb.DeleteRange(ctx, call, 0, []byte("foo"), nil); err != nil {
			t.Fatalf("tb.DeleteRange failed: %v", err)
		}
		expected = append(expected, deleteOp)
		// SetPrefixPermissions.
		if err := tb.SetPrefixPermissions(ctx, call, 0, "foobaz", perms); err != nil {
			t.Fatalf("tb.SetPrefixPermissions failed: %v", err)
		}
		expected = append(expected, putOp(st, tb.prefixPermsKey("foobaz"), tb.prefixPermsKey("foo"), permVersion))
		// SetPrefixPermissions again.
		permVersion, _ = watchable.GetVersion(ctx, st, []byte(tb.prefixPermsKey("foobaz")))
		if err := tb.SetPrefixPermissions(ctx, call, 0, "foobaz", perms); err != nil {
			t.Fatalf("tb.SetPrefixPermissions failed: %v", err)
		}
		expected = append(expected, putOp(st, tb.prefixPermsKey("foobaz"), tb.prefixPermsKey("foobaz"), permVersion))
		// DeletePrefixPermissions.
		permVersion, _ = watchable.GetVersion(ctx, st, []byte(tb.prefixPermsKey("foobaz")))
		if err := tb.DeletePrefixPermissions(ctx, call, 0, "foobaz"); err != nil {
			t.Fatalf("tb.DeletePrefixPermissions failed: %v", err)
		}
		expected = append(expected, watchable.OpDelete{watchable.DeleteOp{
			Key:         []byte(tb.prefixPermsKey("foobaz")),
			PermKey:     []byte(tb.prefixPermsKey("foobaz")),
			PermVersion: permVersion,
		}})
	}
	expectedIndex := 0
	for {
		var logs []*watchable.LogEntry
		if logs, resumeMarker, _ = watchable.ReadBatchFromLog(st, resumeMarker); logs == nil {
			break
		}
		for _, logRecord := range logs {
			if expectedIndex < len(expected) && reflect.DeepEqual(logRecord.Op, expected[expectedIndex]) {
				expectedIndex++
			}
		}
	}
	if expectedIndex != len(expected) {
		t.Fatalf("only %d out of %d record were found", expectedIndex, len(expected))
	}
}

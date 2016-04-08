// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package control_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
	"v.io/x/lib/gosh"
	_ "v.io/x/ref/runtime/factories/generic"
	_ "v.io/x/ref/runtime/protocols/vine"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/longevity_tests/control"
	"v.io/x/ref/services/syncbase/longevity_tests/model"
)

func TestMain(m *testing.M) {
	gosh.InitMain()
	os.Exit(m.Run())
}

func newController(t *testing.T) (*control.Controller, func()) {
	rootDir, err := ioutil.TempDir("", "control-test-")
	if err != nil {
		t.Fatal(err)
	}
	ctx, shutdown := v23.Init()
	opts := control.Opts{
		DebugOutput: true,
		TB:          t,
		RootDir:     rootDir,
	}
	c, err := control.NewController(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		if err := c.TearDown(); err != nil {
			t.Fatal(err)
		}
		shutdown()
		os.RemoveAll(rootDir)
	}
	return c, cleanup
}

func mountTableIsRunning(t *testing.T, c *control.Controller) bool {
	ctxWithTimeout, cancel := context.WithTimeout(c.InternalCtx(), 1*time.Second)
	defer cancel()
	_, _, err := v23.GetNamespace(ctxWithTimeout).GetPermissions(ctxWithTimeout, "")
	return err == nil
}

func syncbaseIsRunning(t *testing.T, c *control.Controller, name string) bool {
	ctxWithTimeout, cancel := context.WithTimeout(c.InternalCtx(), 1*time.Second)
	defer cancel()
	_, _, err := syncbase.NewService(name).GetPermissions(ctxWithTimeout)
	return err == nil
}

func TestRunEmptyUniverse(t *testing.T) {
	c, cleanup := newController(t)
	defer cleanup()

	u := &model.Universe{}

	if err := c.Run(u); err != nil {
		t.Fatal(err)
	}
	// Check mounttable is running.
	if !mountTableIsRunning(t, c) {
		t.Errorf("expected mounttable to be running but it was not")
	}

	// Calling Run a second time should not fail.
	if err := c.Run(u); err != nil {
		t.Fatal(err)
	}
}

func TestRunUniverseSingleDevice(t *testing.T) {
	c, cleanup := newController(t)
	defer cleanup()

	name := "test-device"
	u := &model.Universe{
		Devices: model.DeviceSet{
			&model.Device{
				Name: name,
			},
		},
	}

	if err := c.Run(u); err != nil {
		t.Fatal(err)
	}
	// Check mounttable is running.
	if !mountTableIsRunning(t, c) {
		t.Errorf("expected mounttable to be running but it was not")
	}
	// Check syncbase is running.
	if !syncbaseIsRunning(t, c, name) {
		t.Errorf("expected syncbase %q to be running but it was not", name)
	}

	// Calling Run a second time should not fail.
	if err := c.Run(u); err != nil {
		t.Fatal(err)
	}
	if !syncbaseIsRunning(t, c, name) {
		t.Errorf("expected syncbase %q to be running but it was not", name)
	}

	// Delete the device from the universe.
	u.Devices = model.DeviceSet{}
	if err := c.Run(u); err != nil {
		t.Fatal(err)
	}

	// Syncbase should no longer be running.
	if syncbaseIsRunning(t, c, name) {
		t.Errorf("expected syncbase %q not to be running but it was", name)
	}
}

func TestRunUniverseTwoDevices(t *testing.T) {
	c, cleanup := newController(t)
	defer cleanup()

	d1 := &model.Device{
		Name: "test-device-1",
	}
	d2 := &model.Device{
		Name: "test-device-2",
	}

	// Initially universe has devices unconnected.
	uDisconnected := &model.Universe{
		Devices: model.DeviceSet{d1, d2},
		Topology: model.Topology{
			d1: model.DeviceSet{d1},
			d2: model.DeviceSet{d2},
		},
	}
	if err := c.Run(uDisconnected); err != nil {
		t.Fatal(err)
	}
	// Check that the devices are not syncing.
	if syncbasesCanSync(t, c, d1.Name, d2.Name) {
		t.Fatalf("expected syncbases %v and %v not to sync but they did", d1.Name, d2.Name)
	}

	// Connect the two devices.
	uConnected := &model.Universe{
		Devices: model.DeviceSet{d1, d2},
		Topology: model.Topology{
			d1: model.DeviceSet{d1, d2},
			d2: model.DeviceSet{d1, d2},
		},
	}
	if err := c.Run(uConnected); err != nil {
		t.Fatal(err)
	}
	// Check that the devices are syncing.
	if !syncbasesCanSync(t, c, d1.Name, d2.Name) {
		t.Fatalf("expected syncbases %v and %v to sync but they did not", d1.Name, d2.Name)
	}

	// Revert back to unconnected devices.
	if err := c.Run(uDisconnected); err != nil {
		t.Fatal(err)
	}
	// Check that the devices are not syncing.
	if syncbasesCanSync(t, c, d1.Name, d2.Name) {
		t.Fatalf("expected syncbases %v and %v not to sync but they did", d1.Name, d2.Name)
	}
}

var counter int

// TODO(nlacasse): Once the controller has more client-logic built-in for
// creating databases, collections, syncgroups, etc., see if this test can be
// simplified.
func syncbasesCanSync(t *testing.T, c *control.Controller, sb1Name, sb2Name string) bool {
	ctx := c.InternalCtx()
	sb1Service, sb2Service := syncbase.NewService(sb1Name), syncbase.NewService(sb2Name)

	openPerms := access.Permissions{
		"Admin": access.AccessList{In: []security.BlessingPattern{"..."}},
		"Read":  access.AccessList{In: []security.BlessingPattern{"..."}},
		"Write": access.AccessList{In: []security.BlessingPattern{"..."}},
	}

	// Create databases on both syncbase servers.
	counter++
	dbName := fmt.Sprintf("test_database_%d", counter)
	sb1Db := sb1Service.Database(ctx, dbName, nil)
	if err := sb1Db.Create(ctx, openPerms); err != nil {
		t.Fatal(err)
	}
	sb2Db := sb2Service.Database(ctx, dbName, nil)
	if err := sb2Db.Create(ctx, openPerms); err != nil {
		t.Fatal(err)
	}

	// Create collections on both syncbase servers.
	counter++
	colName := fmt.Sprintf("test_collection_%d", counter)
	sb1Col := sb1Db.Collection(colName)
	if err := sb1Col.Create(ctx, openPerms); err != nil {
		t.Fatal(err)
	}
	sb2Col := sb2Db.Collection(colName)
	if err := sb2Col.Create(ctx, openPerms); err != nil {
		t.Fatal(err)
	}

	// Create a syncgroup on the first syncbase.
	counter++
	sgName := fmt.Sprintf("test_sg_%d", counter)
	fullSgName := naming.Join(sb1Name, common.SyncbaseSuffix, sgName)
	mounttable := v23.GetNamespace(ctx).Roots()[0]
	sbSpec := wire.SyncgroupSpec{
		Description: "test syncgroup",
		Perms:       openPerms,
		Prefixes: []wire.CollectionRow{
			wire.CollectionRow{
				CollectionName: colName,
				Row:            "",
			},
		},
		MountTables: []string{mounttable},
	}
	sb1Sg := sb1Db.Syncgroup(fullSgName)
	if err := sb1Sg.Create(ctx, sbSpec, wire.SyncgroupMemberInfo{}); err != nil {
		t.Fatal(err)
	}

	// If second syncbase can join the syncgroup, they are connected.
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sb2Sg := sb2Db.Syncgroup(fullSgName)
	_, err := sb2Sg.Join(ctxWithTimeout, wire.SyncgroupMemberInfo{})
	return err == nil
}

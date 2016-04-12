// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package model defines functions for generating random sets of model
// databases, devices, and users that will be simulated in a syncbase longevity
// test.
package model

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func randomName(prefix string) string {
	return fmt.Sprintf("%s-%08x", prefix, rand.Int31())
}

// =========
// Databases
// =========

// Database represents a syncbase database.  Each database corresponds to a
// single app.
// TODO(nlacasse): ACLs.
// TODO(nlacasse): Collections.
type Database struct {
	// Name of the database.
	Name string
}

// DatabaseSet represents a set of Databases.
// TODO(nlacasse): Consider using a map here if uniqueness becomes an issue.
type DatabaseSet []*Database

// GenerateDatabaseSet generates a DatabaseSet with n databases.
func GenerateDatabaseSet(n int) DatabaseSet {
	dbs := DatabaseSet{}
	for i := 0; i < n; i++ {
		db := &Database{
			Name: randomName("db"),
		}
		dbs = append(dbs, db)
	}
	return dbs
}

// RandomSubset returns a random subset of the DatabaseSet with at least min
// and at most max databases.
func (dbs DatabaseSet) RandomSubset(min, max int) DatabaseSet {
	if min < 0 || min > len(dbs) || min > max {
		panic(fmt.Errorf("invalid arguments to RandomSubset: min=%v max=%v len(dbs)=%v", min, max, len(dbs)))
	}
	if max > len(dbs) {
		max = len(dbs)
	}
	n := min + rand.Intn(max-min+1)
	subset := make(DatabaseSet, n)
	for i, j := range rand.Perm(len(dbs))[:n] {
		subset[i] = dbs[j]
	}
	return subset
}

func (dbs DatabaseSet) String() string {
	r := make([]string, len(dbs))
	for i, j := range dbs {
		r[i] = j.Name
	}
	return fmt.Sprintf("[%s]", strings.Join(r, ", "))
}

// =======
// Devices
// =======

// ConnectivitySpec specifies the network connectivity of the device.
type ConnectivitySpec string

const (
	Online  ConnectivitySpec = "online"
	Offline ConnectivitySpec = "offline"
	// TODO(nlacasse): Add specs for latency, bandwidth, etc.
)

// DeviceSpec specifies the possible types of devices.
type DeviceSpec struct {
	// Maximum number of databases allowed on the device.
	MaxDatabases int
	// Types of connectivity allowed for the database.
	ConnectivitySpecs []ConnectivitySpec
}

// Some common DeviceSpecs.  It would be nice if these were consts, but go won't allow that.
var (
	LaptopSpec = DeviceSpec{
		MaxDatabases:      10,
		ConnectivitySpecs: []ConnectivitySpec{"online", "offline"},
	}

	PhoneSpec = DeviceSpec{
		MaxDatabases:      5,
		ConnectivitySpecs: []ConnectivitySpec{"online", "offline"},
	}

	CloudSpec = DeviceSpec{
		MaxDatabases:      100,
		ConnectivitySpecs: []ConnectivitySpec{"online"},
	}
	// TODO(nlacasse): Add more DeviceSpecs for tablet, desktop, camera,
	// wearable, etc.
)

// Device represents a device.
type Device struct {
	// Name of the device.
	Name string
	// Databases inluded on the device.
	Databases DatabaseSet
	// The device's spec.
	Spec DeviceSpec
	// Current connectivity spec for the device.  This value must be included
	// in Spec.ConnectivitySpecs.
	CurrentConnectivity ConnectivitySpec
}

func (d Device) String() string {
	return d.Name
}

// DeviceSet is a set of devices.
// TODO(nlacasse): Consider using a map here if uniqueness becomes an issue.
type DeviceSet []*Device

// GenerateDeviceSet generates a device set of size n.  The device spec for
// each device is chosen randomly from specs, and each device is given a
// non-empty set of databases taken from databases argument, obeying the maxium
// for the device spec.
func GenerateDeviceSet(n int, databases DatabaseSet, specs []DeviceSpec) DeviceSet {
	ds := DeviceSet{}
	for i := 0; i < n; i++ {
		// Pick a random spec.
		idx := rand.Intn(len(specs))
		spec := specs[idx]
		databases := databases.RandomSubset(1, spec.MaxDatabases)

		d := &Device{
			Name:      randomName("device"),
			Databases: databases,
			Spec:      spec,
			// Pick a random ConnectivitySpec from those allowed by the
			// DeviceSpec.
			CurrentConnectivity: spec.ConnectivitySpecs[rand.Intn(len(spec.ConnectivitySpecs))],
		}
		ds = append(ds, d)
	}
	return ds
}

func (ds DeviceSet) String() string {
	r := make([]string, len(ds))
	for i, j := range ds {
		r[i] = j.String()
	}
	return fmt.Sprintf("[%s]", strings.Join(r, ", "))
}

// Topology is an adjacency matrix specifying the connection type between
// devices.
// TODO(nlacasse): For now we only specify which devices are reachable by each
// device.
type Topology map[*Device]DeviceSet

// GenerateTopology generates a Topology on the given DeviceSet.  The affinity
// argument specifies the probability that any two devices are connected.  We
// ensure that the generated topology is symmetric, but we do *not* guarantee
// that it is connected.
// TODO(nlacasse): Have more fine-grained affinity taking into account the
// DeviceSpec of each device.  E.g. Desktop is likely to be connected to the
// cloud, but less likely to be connected to a watch.
func GenerateTopology(devices DeviceSet, affinity float64) Topology {
	top := Topology{}
	for i, d1 := range devices {
		// All devices are connected to themselves.
		top[d1] = append(top[d1], d1)
		for _, d2 := range devices[i+1:] {
			connected := rand.Float64() <= affinity
			if connected {
				top[d1] = append(top[d1], d2)
				top[d2] = append(top[d2], d1)
			}
		}
	}
	return top
}

// =====
// Users
// =====

// User represents a user.
type User struct {
	// The user's name.
	Name string
	// The user's Databases.
	Databases DatabaseSet
	// The user's devices.  All databases in the user's devices will be
	// included in the user's Databases.
	Devices DeviceSet
}

// UserSet is a set of users.
type UserSet []*User

// UserOpts specifies the options to use when creating a random user.
type UserOpts struct {
	MaxDatabases int
	MinDatabases int
	MaxDevices   int
	MinDevices   int
}

// GenerateUser generates a random user with nonempty set of databases from the
// given database set and n devices.
// TODO(nlacasse): Should DatabaseSet be in UserOpts?
func GenerateUser(dbs DatabaseSet, opts UserOpts) *User {
	// TODO(nlacasse): Make this a parameter?
	specs := []DeviceSpec{
		LaptopSpec,
		PhoneSpec,
		CloudSpec,
	}

	databases := dbs.RandomSubset(opts.MinDatabases, opts.MaxDatabases)
	numDevices := opts.MinDevices
	if opts.MaxDevices > opts.MinDevices {
		numDevices += rand.Intn(opts.MaxDevices - opts.MinDevices)
	}
	devices := GenerateDeviceSet(numDevices, databases, specs)

	return &User{
		Name:      randomName("user"),
		Databases: databases,
		Devices:   devices,
	}
}

// ========
// Universe
// ========

type Universe struct {
	// All databases in the universe.
	Databases DatabaseSet
	// All users in the universe.
	Users UserSet
	// Description of device connectivity.
	Topology Topology
}

// UniverseOpts specifies the options to use when creating a random universe.
type UniverseOpts struct {
	// Probability that any two devices are connected
	DeviceAffinity float64
	// Number of databases in the universe.
	NumDatabases int
	// Number of users in the universe.
	NumUsers int
	// Maximum number of databases for any user.
	MaxDatabasesPerUser int
	// Minimum number of databases for any user.
	MinDatabasesPerUser int
	// Maximum number of devices for any user.
	MaxDevicesPerUser int
	// Minimum number of devices for any user.
	MinDevicesPerUser int
}

func GenerateUniverse(opts UniverseOpts) Universe {
	dbs := GenerateDatabaseSet(opts.NumDatabases)
	userOpts := UserOpts{
		MaxDatabases: opts.MaxDatabasesPerUser,
		MinDatabases: opts.MinDatabasesPerUser,
		MaxDevices:   opts.MaxDevicesPerUser,
		MinDevices:   opts.MinDevicesPerUser,
	}
	users := UserSet{}
	devices := DeviceSet{}
	for i := 0; i < opts.NumUsers; i++ {
		user := GenerateUser(dbs, userOpts)
		users = append(users, user)
		devices = append(devices, user.Devices...)
	}

	return Universe{
		Databases: dbs,
		Users:     users,
		Topology:  GenerateTopology(devices, opts.DeviceAffinity),
	}
}

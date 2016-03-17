// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syncbaselib

import (
	"flag"
)

type Opts struct {
	Name                  string
	RootDir               string
	Engine                string
	PublishInNeighborhood bool
	DevMode               bool
	CpuProfile            string
}

// TODO(sadovsky): Change flag default values to zero values where reasonable.
// In particular, switch to "-skip-publish-nh", make empty "-root-dir" result in
// a new directory under TMPDIR, and make empty "-engine" result in using the
// default engine, leveldb.
func (o *Opts) InitFlags(f *flag.FlagSet) {
	f.StringVar(&o.Name, "name", "", "Name to mount at.")
	f.StringVar(&o.RootDir, "root-dir", "/var/lib/syncbase", "Root dir for storage engines and other data.")
	f.StringVar(&o.Engine, "engine", "leveldb", "Storage engine to use: memstore or leveldb.")
	f.BoolVar(&o.PublishInNeighborhood, "publish-nh", true, "Whether to publish in the neighborhood.")
	f.BoolVar(&o.DevMode, "dev", false, "Whether to run in development mode; required for RPCs such as Service.DevModeUpdateVClock.")
	f.StringVar(&o.CpuProfile, "cpuprofile", "", "If specified, write the cpu profile to the given filename.")
}

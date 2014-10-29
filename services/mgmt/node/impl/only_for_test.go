package impl

import (
	"flag"
	"os"
	"path/filepath"

	"veyron.io/veyron/veyron2/vlog"
)

// This file contains code in the impl package that we only want built for tests
// (it exposes public API methods that we don't want to normally expose).

var mockIsSetuid = flag.Bool("mocksetuid", false, "set flag to pretend to have a helper with setuid permissions")

func (c *callbackState) leaking() bool {
	c.Lock()
	defer c.Unlock()
	return len(c.channels) > 0
}

func (d *dispatcher) Leaking() bool {
	return d.internal.callback.leaking()
}

func init() {
	cleanupDir = func(dir, helper string) {
		parentDir, base := filepath.Dir(dir), filepath.Base(dir)
		var renamed string
		if helper != "" {
			renamed = filepath.Join(parentDir, "helper_deleted_"+base)
		} else {
			renamed = filepath.Join(parentDir, "deleted_"+base)
		}
		if err := os.Rename(dir, renamed); err != nil {
			vlog.Errorf("Rename(%v, %v) failed: %v", dir, renamed, err)
		}
	}
	isSetuid = possiblyMockIsSetuid
}

func possiblyMockIsSetuid(fileStat os.FileInfo) bool {
	vlog.VI(2).Infof("Mock isSetuid is reporting: %v", *mockIsSetuid)
	return *mockIsSetuid
}

func WrapBaseCleanupDir(path, helper string) {
	baseCleanupDir(path, helper)
}

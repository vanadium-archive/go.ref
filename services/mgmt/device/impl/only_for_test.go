package impl

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/services/mgmt/device"
	"v.io/core/veyron2/vlog"
)

// This file contains code in the impl package that we only want built for tests
// (it exposes public API methods that we don't want to normally expose).

var mockIsSetuid = flag.Bool("mocksetuid", false, "set flag to pretend to have a helper with setuid permissions")

func (c *callbackState) leaking() bool {
	c.Lock()
	defer c.Unlock()
	return len(c.channels) > 0
}

func DispatcherLeaking(d ipc.Dispatcher) bool {
	switch obj := d.(type) {
	case *dispatcher:
		return obj.internal.callback.leaking()
	case *testModeDispatcher:
		return obj.realDispatcher.(*dispatcher).internal.callback.leaking()
	default:
		panic(fmt.Sprintf("unexpected type: %T", d))
	}
}

func init() {
	cleanupDir = func(dir, helper string) {
		if dir == "" {
			return
		}
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

	Describe = func() (descr device.Description, err error) {
		return device.Description{Profiles: map[string]struct{}{"test-profile": struct{}{}}}, nil
	}
}

func possiblyMockIsSetuid(fileStat os.FileInfo) bool {
	vlog.VI(2).Infof("Mock isSetuid is reporting: %v", *mockIsSetuid)
	return *mockIsSetuid
}

func WrapBaseCleanupDir(path, helper string) {
	baseCleanupDir(path, helper)
}

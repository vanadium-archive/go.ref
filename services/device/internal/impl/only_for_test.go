// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"fmt"
	"os"
	"path/filepath"

	"v.io/v23/rpc"
	"v.io/x/lib/vlog"
)

// This file contains code in the impl package that we only want built for tests
// (it exposes public API methods that we don't want to normally expose).

func (c *callbackState) leaking() bool {
	c.Lock()
	defer c.Unlock()
	return len(c.channels) > 0
}

func DispatcherLeaking(d rpc.Dispatcher) bool {
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
}

func WrapBaseCleanupDir(path, helper string) {
	baseCleanupDir(path, helper)
}

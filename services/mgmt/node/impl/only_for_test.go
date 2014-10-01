package impl

import (
	"os"
	"path/filepath"

	"veyron.io/veyron/veyron2/vlog"
)

// This file contains code in the impl package that we only want built for tests
// (it exposes public API methods that we don't want to normally expose).

func (c *callbackState) leaking() bool {
	c.Lock()
	defer c.Unlock()
	return len(c.channels) > 0
}

func (d *dispatcher) Leaking() bool {
	return d.internal.callback.leaking()
}

func init() {
	cleanupDir = func(dir string) {
		parentDir, base := filepath.Dir(dir), filepath.Base(dir)
		renamed := filepath.Join(parentDir, "deleted_"+base)
		if err := os.Rename(dir, renamed); err != nil {
			vlog.Errorf("Rename(%v, %v) failed: %v", dir, renamed, err)
		}
	}
}

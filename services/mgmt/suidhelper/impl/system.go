// +build linux darwin

package impl

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// Chown is only availabe on UNIX platforms so this file has a build
// restriction.
func (hw *WorkParameters) Chown() error {
	chown := func(path string, _ os.FileInfo, inerr error) error {
		if inerr != nil {
			return inerr
		}
		if hw.dryrun {
			log.Printf("[dryrun] os.Chown(%s, %d, %d)", path, hw.uid, hw.gid)
			return nil
		}
		return os.Chown(path, hw.uid, hw.gid)
	}

	for _, p := range []string{hw.workspace, hw.logDir} {
		// TODO(rjkroege): Ensure that the device manager can read log entries.
		if err := filepath.Walk(p, chown); err != nil {
			return fmt.Errorf("os.Chown(%s, %d, %d) failed: %v", p, hw.uid, hw.gid, err)
		}
	}
	return nil
}

func (hw *WorkParameters) Exec() error {
	if hw.dryrun {
		log.Printf("[dryrun] syscall.Setgid(%d)", hw.gid)
		log.Printf("[dryrun] syscall.Setuid(%d)", hw.uid)
	} else {
		if err := syscall.Setgid(hw.gid); err != nil {
			return fmt.Errorf("syscall.Setgid(%d) failed: %v", hw.gid, err)
		}
		if err := syscall.Setuid(hw.uid); err != nil {
			return fmt.Errorf("syscall.Setuid(%d) failed: %v", hw.uid, err)
		}
	}
	return syscall.Exec(hw.argv0, hw.argv, hw.envv)
}

func (hw *WorkParameters) Remove() error {
	for _, p := range hw.argv {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("os.RemoveAll(%s) failed: %v", p, err)
		}
	}
	return nil
}

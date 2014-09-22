// +build linux darwin

package impl

import (
	"log"
	"syscall"
)

// Chown is only availabe on UNIX platforms so this file has a build
// restriction.
func (hw *WorkParameters) Chown() error {
	// TODO(rjkroege): Insert the actual code to chown in a subsequent CL.
	log.Printf("chown %s %s\n", hw.uid, hw.workspace)
	log.Printf("chown %s %s\n", hw.uid, hw.stdoutLog)
	log.Printf("chown %s %s\n", hw.uid, hw.stderrLog)

	return nil
}

func (hw *WorkParameters) Exec() error {
	log.Printf("should be Exec-ing work parameters: %#v\n", hw)
	log.Printf("su %s\n", hw.uid)
	log.Printf("exec %s %v", hw.argv0, hw.argv)
	log.Printf("env: %v", hw.envv)

	// TODO(rjkroege): Insert the actual code to change uid in a subsquent CL.
	return syscall.Exec(hw.argv0, hw.argv, hw.envv)
}

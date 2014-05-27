// Package parent provides hooks for parent code, that is not part of a test,
// to safely interact with blackbox tests when run under them. This happens
// for example with veyron/lib/exec library that also spawns subprocesses
// using its own protocol and may also be run as a blackbox test.
package parent

import (
	"os"
	"os/exec"
	"sync"
)

// BlackboxTest returns true if the environment variables passed to
// it contain the variable (GO_WANT_HELPER_PROCESS_BLACKBOX=1) indicating that
// a blackbox test is configured.
func BlackboxTest(env []string) bool {
	for _, v := range env {
		if v == "GO_WANT_HELPER_PROCESS_BLACKBOX=1" {
			return true
		}
	}
	return false
}

type pipeList struct {
	sync.Mutex
	writers []*os.File
}

var pipes pipeList

// InitBlacboxParent initializes the exec.Command instance passed in for
// use with a process that is to be run as blackbox test. This is needed
// for processes, such as the node manager, which want to run subprocesses
// from within blackbox tests but are not themselves test code.
// It must be called before any changes are made to ExtraFiles since it will
// use the first entry for itself, overwriting anything that's there.
func InitBlackboxParent(cmd *exec.Cmd) error {
	reader, writer, err := os.Pipe()
	if err != nil {
		return err

	}
	cmd.ExtraFiles = []*os.File{reader}
	if err != nil {
		return err
	}
	// Keep a reference to the writers to prevent GC from closing them
	// and thus causing the child to terminate.
	pipes.Lock()
	pipes.writers = append(pipes.writers, writer)
	pipes.Unlock()
	return nil
}

package integration

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/modules/core"
)

// BuildPkgs returns a path to a directory that contains the built
// binaries for the given set of packages and a function that should
// be invoked to clean up the build artifacts. Note that the clients
// of this function should not modify the contents of this directory
// directly and instead defer to the cleanup function.
func BuildPkgs(pkgs []string) (string, func(), error) {
	// The VEYRON_INTEGRATION_BIN_DIR environment variable can be
	// used to identify a directory that multiple integration
	// tests can use to share binaries. Whoever sets this
	// environment variable is responsible for cleaning up the
	// directory it points to.
	binDir, cleanupFn := os.Getenv("VEYRON_INTEGRATION_BIN_DIR"), func() {}
	if binDir == "" {
		// If the aforementioned environment variable is not
		// set, the given packages are built in a temporary
		// directory, which the cleanup function removes.
		tmpDir, err := ioutil.TempDir("", "")
		if err != nil {
			return "", nil, fmt.Errorf("TempDir() failed: %v", err)
		}
		binDir, cleanupFn = tmpDir, func() { os.RemoveAll(tmpDir) }
	}
	for _, pkg := range pkgs {
		binFile := filepath.Join(binDir, path.Base(pkg))
		if _, err := os.Stat(binFile); err != nil {
			if !os.IsNotExist(err) {
				return "", nil, err
			}
			cmd := exec.Command("veyron", "go", "build", "-o", filepath.Join(binDir, path.Base(pkg)), pkg)
			if err := cmd.Run(); err != nil {
				return "", nil, err
			}
		}
	}
	return binDir, cleanupFn, nil
}

// StartRootMT uses the given shell to start a root mount table and
// returns a handle for the started command along with the object name
// of the mount table.
func StartRootMT(shell *modules.Shell) (modules.Handle, string, error) {
	handle, err := shell.Start(core.RootMTCommand, nil, "--", "--veyron.tcp.address=127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	s := expect.NewSession(nil, handle.Stdout(), time.Second)
	name := s.ExpectVar("MT_NAME")
	if err := s.Error(); err != nil {
		return nil, "", err
	}
	s.ExpectVar("MT_ADDR")
	if err := s.Error(); err != nil {
		return nil, "", err
	}
	s.ExpectVar("PID")
	if err := s.Error(); err != nil {
		return nil, "", err
	}
	return handle, name, nil
}

// StartServer starts a veyron server using the given binary and
// arguments, waiting for the server to successfully mount itself in
// the mount table.
//
// TODO(jsimsa,sadovsky): Use an instance of modules.Shell to start
// and manage the server process to prevent leaking processes when
// its parent terminates unexpectedly.
func StartServer(bin string, args []string) (*os.Process, error) {
	args = append(args, "-logtostderr", "-vmodule=publisher=2")
	cmd := exec.Command(bin, args...)
	outPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	// TODO(jsimsa): Consider using the veyron exec library to
	// facilitate coordination and communication between the
	// parent and the child process.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%q failed: %v", strings.Join(cmd.Args, " "), err)
	}
	// Wait for the server to mount both its tcp and ws endpoint.
	ready := make(chan struct{}, 1)
	go func() {
		defer outPipe.Close()
		scanner := bufio.NewScanner(outPipe)
		mounts := 0
		for scanner.Scan() {
			line := scanner.Text()
			// TODO(cnicolaou): find a better way of synchronizing with
			// the child process, this is way too fragile.
			if strings.Index(line, "ipc pub: mount") != -1 {
				mounts++
				if mounts == 1 {
					close(ready)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Scan() failed: %v\n", err)
		}
	}()
	select {
	case <-ready:
		return cmd.Process, nil
	case <-time.After(time.Minute):
		cmd.Process.Kill()
		return nil, fmt.Errorf("timed out waiting for %q to mount itself", strings.Join(cmd.Args, " "))
	}
}

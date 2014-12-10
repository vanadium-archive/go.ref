package testutil

import (
	"os"
	"strconv"
	"testing"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/flags/consts"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/modules/core"
	"v.io/core/veyron/lib/testutil"
	"v.io/core/veyron/lib/testutil/security"
)

// StartRootMT sets up a root mount table for tests.
func StartRootMT(t *testing.T, sh *modules.Shell) (string, modules.Handle) {
	h, err := sh.Start(core.RootMTCommand, nil, "--", "--veyron.tcp.address=127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start root mount table: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), ExpectTimeout)
	s.ExpectVar("PID")
	rootName := s.ExpectVar("MT_NAME")
	if t.Failed() {
		t.Fatalf("failed to read mt name: %s", s.Error())
	}
	return rootName, h
}

// CredentialsForChild creates credentials for a child process.
func CredentialsForChild(rt veyron2.Runtime, blessing string) (string, []string) {
	creds, _ := security.ForkCredentials(rt.Principal(), blessing)
	return creds, []string{consts.VeyronCredentials + "=" + creds}
}

// SetNSRoots sets the roots for the local runtime's namespace.
func SetNSRoots(t *testing.T, rt veyron2.Runtime, roots ...string) {
	if err := rt.Namespace().SetRoots(roots...); err != nil {
		t.Fatalf(testutil.FormatLogLine(3, "SetRoots(%v) failed with %v", roots, err))
	}
}

// CreateShellAndMountTable builds a new modules shell and its
// associated mount table.
func CreateShellAndMountTable(t *testing.T, rt veyron2.Runtime) (*modules.Shell, func()) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// The shell, will, by default share credentials with its children.
	sh.ClearVar(consts.VeyronCredentials)

	mtName, mtHandle := StartRootMT(t, sh)
	vlog.VI(1).Infof("Started shell mounttable with name %v", mtName)
	// Make sure the root mount table is the last process to be shutdown
	// since the others will likely want to communicate with it during
	// their shutdown process
	sh.Forget(mtHandle)

	// TODO(caprita): Define a GetNamespaceRootsCommand in modules/core and
	// use that?
	oldNamespaceRoots := rt.Namespace().Roots()
	fn := func() {
		vlog.VI(1).Info("------------ CLEANUP ------------")
		vlog.VI(1).Info("---------------------------------")
		vlog.VI(1).Info("--(cleaning up shell)------------")
		if err := sh.Cleanup(os.Stdout, os.Stderr); err != nil {
			t.Fatalf("sh.Cleanup failed with %v", err)
		}
		vlog.VI(1).Info("--(done cleaning up shell)-------")
		vlog.VI(1).Info("--(shutting down root mt)--------")
		if err := mtHandle.Shutdown(os.Stdout, os.Stderr); err != nil {
			t.Fatalf("mtHandle.Shutdown failed with %v", err)
		}
		vlog.VI(1).Info("--(done shutting down root mt)---")
		vlog.VI(1).Info("--------- DONE CLEANUP ----------")
		SetNSRoots(t, rt, oldNamespaceRoots...)
	}
	SetNSRoots(t, rt, mtName)
	sh.SetVar(consts.NamespaceRootPrefix, mtName)
	return sh, fn
}

// RunShellCommand runs an external command using the modules system.
func RunShellCommand(t *testing.T, sh *modules.Shell, env []string, cmd string, args ...string) (modules.Handle, *expect.Session) {
	h, err := sh.Start(cmd, env, args...)
	if err != nil {
		t.Fatalf(testutil.FormatLogLine(2, "failed to start %q: %s", cmd, err))
		return nil, nil
	}
	s := expect.NewSession(t, h.Stdout(), ExpectTimeout)
	s.SetVerbosity(testing.Verbose())
	return h, s
}

// NewServer creates a new server.
func NewServer(rt veyron2.Runtime) (ipc.Server, string) {
	server, err := rt.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	spec := ipc.ListenSpec{Addrs: ipc.ListenAddrs{{"tcp", "127.0.0.1:0"}}}
	endpoints, err := server.Listen(spec)
	if err != nil {
		vlog.Fatalf("Listen(%s) failed: %v", spec, err)
	}
	return server, endpoints[0].String()
}

// NewRuntime makes an instance of the runtime.
func NewRuntime(t *testing.T, ort veyron2.Runtime, opts ...veyron2.ROpt) veyron2.Runtime {
	runtime, err := rt.New(opts...)
	if err != nil {
		t.Fatalf("rt.New() failed: %v", err)
	}
	runtime.Namespace().SetRoots(ort.Namespace().Roots()[0])
	return runtime
}

// ReadPID waits for the "ready:<PID>" line from the child and parses out the
// PID of the child.
func ReadPID(t *testing.T, s *expect.Session) int {
	m := s.ExpectRE("ready:([0-9]+)", -1)
	if len(m) == 1 && len(m[0]) == 2 {
		pid, err := strconv.Atoi(m[0][1])
		if err != nil {
			t.Fatalf(testutil.FormatLogLine(2, "Atoi(%q) failed: %v", m[0][1], err))
		}
		return pid
	}
	t.Fatalf(testutil.FormatLogLine(2, "failed to extract pid: %v", m))
	return 0
}

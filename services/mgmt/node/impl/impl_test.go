// TODO(caprita): This file is becoming unmanageable; split into several test
// files.
// TODO(rjkroege): Add a more extensive unit test case to exercise ACL logic.

package impl_test

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	goexec "os/exec"
	"os/user"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/services/mgmt/logreader"
	"veyron.io/veyron/veyron2/services/mgmt/node"
	"veyron.io/veyron/veyron2/services/mgmt/pprof"
	"veyron.io/veyron/veyron2/services/mgmt/stats"
	"veyron.io/veyron/veyron2/services/security/access"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/lib/testutil"
	tsecurity "veyron.io/veyron/veyron/lib/testutil/security"
	"veyron.io/veyron/veyron/services/mgmt/node/config"
	"veyron.io/veyron/veyron/services/mgmt/node/impl"
	suidhelper "veyron.io/veyron/veyron/services/mgmt/suidhelper/impl"
)

const (
	execScriptCmd  = "execScriptCmd"
	nodeManagerCmd = "nodeManager"
	appCmd         = "app"
	installerCmd   = "installer"
)

func init() {
	// TODO(rthellend): Remove when vom2 is ready.
	vom.Register(&naming.VDLMountedServer{})

	modules.RegisterChild(execScriptCmd, "", execScript)
	modules.RegisterChild(nodeManagerCmd, "", nodeManager)
	modules.RegisterChild(appCmd, "", app)
	modules.RegisterChild(installerCmd, "", install)
	testutil.Init()

	if modules.IsModulesProcess() {
		return
	}
	initRT()
}

func initRT() {
	rt.Init()
	// Disable the cache because we will be manipulating/using the namespace
	// across multiple processes and want predictable behaviour without
	// relying on timeouts.
	rt.R().Namespace().CacheCtl(naming.DisableCache(true))
}

// TestHelperProcess is the entrypoint for the modules commands in a
// a test subprocess.
func TestHelperProcess(t *testing.T) {
	initRT()
	modules.DispatchInTest()
}

// TestSuidHelper is testing boilerplate for suidhelper that does not
// invoke rt.Init() because the suidhelper is not a Veyron application.
func TestSuidHelper(t *testing.T) {
	if os.Getenv("VEYRON_SUIDHELPER_TEST") != "1" {
		return
	}
	vlog.VI(1).Infof("TestSuidHelper starting")

	if err := suidhelper.Run(os.Environ()); err != nil {
		vlog.Fatalf("Failed to Run() setuidhelper: %v", err)
	}
}

// execScript launches the script passed as argument.
func execScript(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	args = args[1:]
	if want, got := 1, len(args); want != got {
		vlog.Fatalf("execScript expected %d arguments, got %d instead", want, got)
	}
	script := args[0]
	osenv := []string{}
	if env["PAUSE_BEFORE_STOP"] == "1" {
		osenv = append(osenv, "PAUSE_BEFORE_STOP=1")
	}

	cmd := goexec.Cmd{
		Path:   script,
		Env:    osenv,
		Stdin:  stdin,
		Stderr: stderr,
		Stdout: stdout,
	}

	return cmd.Run()
}

// nodeManager sets up a node manager server.  It accepts the name to publish
// the server under as an argument.  Additional arguments can optionally specify
// node manager config settings.
func nodeManager(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	args = args[1:]
	if len(args) == 0 {
		vlog.Fatalf("nodeManager expected at least an argument")
	}
	publishName := args[0]
	args = args[1:]

	defer fmt.Fprintf(stdout, "%v terminating\n", publishName)
	defer vlog.VI(1).Infof("%v terminating", publishName)
	defer rt.R().Cleanup()
	server, endpoint := newServer()
	defer server.Stop()
	name := naming.JoinAddressName(endpoint, "")
	vlog.VI(1).Infof("Node manager name: %v", name)

	// Satisfy the contract described in doc.go by passing the config state
	// through to the node manager dispatcher constructor.
	configState, err := config.Load()
	if err != nil {
		vlog.Fatalf("Failed to decode config state: %v", err)
	}
	configState.Name = name

	// This exemplifies how to override or set specific config fields, if,
	// for example, the node manager is invoked 'by hand' instead of via a
	// script prepared by a previous version of the node manager.
	if len(args) > 0 {
		if want, got := 4, len(args); want != got {
			vlog.Fatalf("expected %d additional arguments, got %d instead", want, got)
		}
		configState.Root, configState.Helper, configState.Origin, configState.CurrentLink = args[0], args[1], args[2], args[3]
	}
	dispatcher, err := impl.NewDispatcher(configState)
	if err != nil {
		vlog.Fatalf("Failed to create node manager dispatcher: %v", err)
	}
	if err := server.ServeDispatcher(publishName, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}
	impl.InvokeCallback(name)

	fmt.Fprintf(stdout, "ready:%d\n", os.Getpid())

	<-signals.ShutdownOnSignals()

	if val, present := env["PAUSE_BEFORE_STOP"]; present && val == "1" {
		modules.WaitForEOF(stdin)
	}
	if dispatcher.Leaking() {
		vlog.Fatalf("node manager leaking resources")
	}
	return nil
}

// install installs the node manager.
func install(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	args = args[1:]
	// args[0] is the entrypoint for the binary to be run from the shell script
	// that SelfInstall will write out.
	entrypoint := args[0]
	// Overwrite the entrypoint in our environment (i.e. the one
	// that got us here), with the one we want written out in the shell
	// script.
	osenv := modules.SetEntryPoint(env, entrypoint)
	if args[1] != "--" {
		vlog.Fatalf("expected '--' immediately following command name")
	}
	args = append([]string{""}, args[2:]...) // skip the cmd and '--'
	if err := impl.SelfInstall(args, osenv); err != nil {
		vlog.Fatalf("SelfInstall failed: %v", err)
		return err
	}
	return nil
}

// appService defines a test service that the test app should be running.
// TODO(caprita): Use this to make calls to the app and verify how Suspend/Stop
// interact with an active service.
type appService struct{}

func (appService) Echo(_ ipc.ServerContext, message string) (string, error) {
	return message, nil
}

func ping() {
	if call, err := rt.R().Client().StartCall(rt.R().NewContext(), "pingserver", "Ping", []interface{}{os.Getenv(suidhelper.SavedArgs)}); err != nil {
		vlog.Fatalf("StartCall failed: %v", err)
	} else if err := call.Finish(); err != nil {
		vlog.Fatalf("Finish failed: %v", err)
	}
}

func app(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	args = args[1:]
	if expected, got := 1, len(args); expected != got {
		vlog.Fatalf("Unexpected number of arguments: expected %d, got %d", expected, got)
	}
	publishName := args[0]

	defer rt.R().Cleanup()
	server, _ := newServer()
	defer server.Stop()
	if err := server.Serve(publishName, new(appService), nil); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}
	// Some of our tests look for log files, so make sure they are flushed
	// to ensure that at least the files exist.
	vlog.FlushLog()
	ping()

	<-signals.ShutdownOnSignals()
	if err := ioutil.WriteFile("testfile", []byte("goodbye world"), 0600); err != nil {
		vlog.Fatalf("Failed to write testfile: %v", err)
	}
	return nil
}

// TODO(rjkroege): generateNodeManagerScript and generateSuidHelperScript have code
// similarity that might benefit from refactoring.
// generateNodeManagerScript is very similar in behavior to generateScript in node_invoker.go.
// However, we chose to re-implement it here for two reasons: (1) avoid making
// generateScript public; and (2) how the test choses to invoke the node manager
// subprocess the first time should be independent of how node manager
// implementation sets up its updated versions.
func generateNodeManagerScript(t *testing.T, root string, args, env []string) string {
	env = impl.VeyronEnvironment(env)
	output := "#!/bin/bash\n"
	output += strings.Join(config.QuoteEnv(env), " ") + " "
	output += strings.Join(args, " ")
	if err := os.MkdirAll(filepath.Join(root, "factory"), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	// Why pigeons? To show that the name we choose for the initial script
	// doesn't matter and in particular is independent of how node manager
	// names its updated version scripts (noded.sh).
	path := filepath.Join(root, "factory", "pigeons.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		t.Fatalf("WriteFile(%v) failed: %v", path, err)
	}
	return path
}

/// readPID waits for the "ready:<PID>" line from the child and parses out the
// PID of the child.
func readPID(t *testing.T, s *expect.Session) int {
	m := s.ExpectRE("ready:([0-9]+)", -1)
	if len(m) == 1 && len(m[0]) == 2 {
		pid, err := strconv.Atoi(m[0][1])
		if err != nil {
			t.Fatalf("%s: Atoi(%q) failed: %v", loc(1), m[0][1], err)
		}
		return pid
	}
	t.Fatalf("%s: failed to extract pid: %v", loc(1), m)
	return 0
}

// TestNodeManagerUpdateAndRevert makes the node manager go through the
// motions of updating itself to newer versions (twice), and reverting itself
// back (twice). It also checks that update and revert fail when they're
// supposed to. The initial node manager is started 'by hand' via a module
// command. Further versions are started through the soft link that the node
// manager itself updates.
func TestNodeManagerUpdateAndRevert(t *testing.T) {
	sh, deferFn := createShellAndMountTable(t)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Current link does not have to live in the root dir, but it's
	// convenient to put it there so we have everything in one place.
	currLink := filepath.Join(root, "current_link")

	crDir, crEnv := credentialsForChild("nodemanager")
	defer os.RemoveAll(crDir)
	nmArgs := []string{"factoryNM", root, "unused_helper", mockApplicationRepoName, currLink}
	args, env := sh.CommandEnvelope(nodeManagerCmd, crEnv, nmArgs...)

	scriptPathFactory := generateNodeManagerScript(t, root, args, env)

	if err := os.Symlink(scriptPathFactory, currLink); err != nil {
		t.Fatalf("Symlink(%q, %q) failed: %v", scriptPathFactory, currLink, err)
	}

	// We instruct the initial node manager that we run to pause before
	// stopping its service, so that we get a chance to verify that
	// attempting an update while another one is ongoing will fail.
	nmPauseBeforeStopEnv := append(crEnv, "PAUSE_BEFORE_STOP=1")

	// Start the initial version of the node manager, the so-called
	// "factory" version. We use the modules-generated command to start it.
	// We could have also used the scriptPathFactory to start it, but
	// this demonstrates that the initial node manager could be started by
	// hand as long as the right initial configuration is passed into the
	// node manager implementation.
	nmh, nms := runShellCommand(t, sh, nmPauseBeforeStopEnv, nodeManagerCmd, nmArgs...)
	defer func() {
		syscall.Kill(nmh.Pid(), syscall.SIGINT)
	}()

	readPID(t, nms)
	resolve(t, "factoryNM", 1) // Verify the node manager has published itself.

	// Simulate an invalid envelope in the application repository.
	*envelope = envelopeFromShell(sh, nmPauseBeforeStopEnv, nodeManagerCmd, "bogus", nmArgs...)

	updateNodeExpectError(t, "factoryNM", verror.BadArg)  // Incorrect title.
	revertNodeExpectError(t, "factoryNM", verror.NoExist) // No previous version available.

	// Set up a second version of the node manager. The information in the
	// envelope will be used by the node manager to stage the next version.
	crDir, crEnv = credentialsForChild("nodemanager")
	defer os.RemoveAll(crDir)
	*envelope = envelopeFromShell(sh, crEnv, nodeManagerCmd, application.NodeManagerTitle, "v2NM")
	updateNode(t, "factoryNM")

	// Current link should have been updated to point to v2.
	evalLink := func() string {
		path, err := filepath.EvalSymlinks(currLink)
		if err != nil {
			t.Fatalf("EvalSymlinks(%v) failed: %v", currLink, err)
		}
		return path
	}
	scriptPathV2 := evalLink()
	if scriptPathFactory == scriptPathV2 {
		t.Fatalf("current link didn't change")
	}

	updateNodeExpectError(t, "factoryNM", verror.Exists) // Update already in progress.

	nmh.CloseStdin()

	nms.Expect("factoryNM terminating")
	nmh.Shutdown(os.Stderr, os.Stderr)

	// A successful update means the node manager has stopped itself.  We
	// relaunch it from the current link.
	resolveExpectNotFound(t, "v2NM") // Ensure a clean slate.

	nmh, nms = runShellCommand(t, sh, nil, execScriptCmd, currLink)

	readPID(t, nms)
	resolve(t, "v2NM", 1) // Current link should have been launching v2.

	// Try issuing an update without changing the envelope in the application
	// repository: this should fail, and current link should be unchanged.
	updateNodeExpectError(t, "v2NM", naming.ErrNoSuchName.ID)
	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Create a third version of the node manager and issue an update.
	crDir, crEnv = credentialsForChild("nodemanager")
	defer os.RemoveAll(crDir)
	*envelope = envelopeFromShell(sh, crEnv, nodeManagerCmd, application.NodeManagerTitle, "v3NM")
	updateNode(t, "v2NM")

	scriptPathV3 := evalLink()
	if scriptPathV3 == scriptPathV2 {
		t.Fatalf("current link didn't change")
	}

	nms.Expect("v2NM terminating")

	nmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, "v3NM") // Ensure a clean slate.

	// Re-lanuch the node manager from current link.
	// We instruct the node manager to pause before stopping its server, so
	// that we can verify that a second revert fails while a revert is in
	// progress.
	nmh, nms = runShellCommand(t, sh, nmPauseBeforeStopEnv, execScriptCmd, currLink)

	readPID(t, nms)
	resolve(t, "v3NM", 1) // Current link should have been launching v3.

	// Revert the node manager to its previous version (v2).
	revertNode(t, "v3NM")
	revertNodeExpectError(t, "v3NM", verror.Exists) // Revert already in progress.
	nmh.CloseStdin()
	nms.Expect("v3NM terminating")
	if evalLink() != scriptPathV2 {
		t.Fatalf("current link was not reverted correctly")
	}
	nmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, "v2NM") // Ensure a clean slate.

	nmh, nms = runShellCommand(t, sh, nil, execScriptCmd, currLink)
	readPID(t, nms)
	resolve(t, "v2NM", 1) // Current link should have been launching v2.

	// Revert the node manager to its previous version (factory).
	revertNode(t, "v2NM")
	nms.Expect("v2NM terminating")
	if evalLink() != scriptPathFactory {
		t.Fatalf("current link was not reverted correctly")
	}
	nmh.Shutdown(os.Stderr, os.Stderr)

	resolveExpectNotFound(t, "factoryNM") // Ensure a clean slate.
	nmh, nms = runShellCommand(t, sh, nil, execScriptCmd, currLink)
	pid := readPID(t, nms)
	resolve(t, "factoryNM", 1) // Current link should have been launching
	syscall.Kill(pid, syscall.SIGINT)
	nms.Expect("factoryNM terminating")
	nms.ExpectEOF()
}

type pingServer chan<- string

// TODO(caprita): Set the timeout in a more principled manner.
const pingTimeout = 20 * time.Second

func (p pingServer) Ping(_ ipc.ServerContext, arg string) {
	p <- arg
}

// setupPingServer creates a server listening for a ping from a child app; it
// returns a channel on which the app's ping message is returned, and a cleanup
// function.
func setupPingServer(t *testing.T) (<-chan string, func()) {
	server, _ := newServer()
	pingCh := make(chan string, 1)
	if err := server.Serve("pingserver", pingServer(pingCh), nil); err != nil {
		t.Fatalf("Serve(%q, <dispatcher>) failed: %v", "pingserver", err)
	}
	return pingCh, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func verifyAppWorkspace(t *testing.T, root, appID, instanceID string) {
	// HACK ALERT: for now, we peek inside the node manager's directory
	// structure (which ought to be opaque) to check for what the app has
	// written to its local root.
	//
	// TODO(caprita): add support to node manager to browse logs/app local
	// root.
	applicationDirName := func(title string) string {
		h := md5.New()
		h.Write([]byte(title))
		hash := strings.TrimRight(base64.URLEncoding.EncodeToString(h.Sum(nil)), "=")
		return "app-" + hash
	}
	components := strings.Split(appID, "/")
	appTitle, installationID := components[0], components[1]
	instanceDir := filepath.Join(root, applicationDirName(appTitle), "installation-"+installationID, "instances", "instance-"+instanceID)
	rootDir := filepath.Join(instanceDir, "root")
	testFile := filepath.Join(rootDir, "testfile")
	if read, err := ioutil.ReadFile(testFile); err != nil {
		t.Fatalf("Failed to read %v: %v", testFile, err)
	} else if want, got := "goodbye world", string(read); want != got {
		t.Fatalf("Expected to read %v, got %v instead", want, got)
	}
	// END HACK
}

// TODO(rjkroege): Consider validating additional parameters.
func verifyHelperArgs(t *testing.T, pingCh <-chan string, username string) {
	var env string
	select {
	case env = <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("%s: failed to get ping", loc(1))
	}
	d := json.NewDecoder(strings.NewReader(env))
	var savedArgs suidhelper.ArgsSavedForTest

	if err := d.Decode(&savedArgs); err != nil {
		t.Fatalf("failed to decode preserved argument %v: %v", env, err)
	}

	if savedArgs.Uname != username {
		t.Fatalf("got username %v, expected username %v", savedArgs.Uname, username)
	}
}

// TestAppLifeCycle installs an app, starts it, suspends it, resumes it, and
// then stops it.
func TestAppLifeCycle(t *testing.T) {
	sh, deferFn := createShellAndMountTable(t)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	crDir, crEnv := credentialsForChild("nodemanager")
	defer os.RemoveAll(crDir)

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	nmh, nms := runShellCommand(t, sh, crEnv, nodeManagerCmd, "nm", root, helperPath, "unused_app_rep_ name", "unused_curr_link")
	readPID(t, nms)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	resolve(t, "pingserver", 1)

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := installApp(t)

	// Start requires the caller to grant a blessing for the app instance.
	if _, err := startAppImpl(t, appID, ""); err == nil || !verror.Is(err, verror.BadArg) {
		t.Fatalf("Start(%v) expected to fail with %v, got %v instead", appID, verror.BadArg, err)
	}

	// Start an instance of the app.
	instance1ID := startApp(t, appID)

	// Wait until the app pings us that it's ready.
	verifyHelperArgs(t, pingCh, userName(t))

	v1EP1 := resolve(t, "appV1", 1)[0]

	// Suspend the app instance.
	suspendApp(t, appID, instance1ID)
	resolveExpectNotFound(t, "appV1")

	resumeApp(t, appID, instance1ID)
	verifyHelperArgs(t, pingCh, userName(t)) // Wait until the app pings us that it's ready.
	oldV1EP1 := v1EP1
	if v1EP1 = resolve(t, "appV1", 1)[0]; v1EP1 == oldV1EP1 {
		t.Fatalf("Expected a new endpoint for the app after suspend/resume")
	}

	// Start a second instance.
	instance2ID := startApp(t, appID)
	verifyHelperArgs(t, pingCh, userName(t)) // Wait until the app pings us that it's ready.

	// There should be two endpoints mounted as "appV1", one for each
	// instance of the app.
	endpoints := resolve(t, "appV1", 2)
	v1EP2 := endpoints[0]
	if endpoints[0] == v1EP1 {
		v1EP2 = endpoints[1]
		if v1EP2 == v1EP1 {
			t.Fatalf("Both endpoints are the same")
		}
	} else if endpoints[1] != v1EP1 {
		t.Fatalf("Second endpoint should have been v1EP1: %v, %v", endpoints, v1EP1)
	}

	// TODO(caprita): test Suspend and Resume, and verify various
	// non-standard combinations (suspend when stopped; resume while still
	// running; stop while suspended).

	// Suspend the first instance.
	suspendApp(t, appID, instance1ID)
	// Only the second instance should still be running and mounted.
	if want, got := v1EP2, resolve(t, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Updating the installation to itself is a no-op.
	updateAppExpectError(t, appID, naming.ErrNoSuchName.ID)

	// Updating the installation should not work with a mismatched title.
	*envelope = envelopeFromShell(sh, nil, appCmd, "bogus")

	updateAppExpectError(t, appID, verror.BadArg)

	// Create a second version of the app and update the app to it.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV2")

	updateApp(t, appID)

	// Second instance should still be running.
	if want, got := v1EP2, resolve(t, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Resume first instance.
	resumeApp(t, appID, instance1ID)
	verifyHelperArgs(t, pingCh, userName(t)) // Wait until the app pings us that it's ready.
	// Both instances should still be running the first version of the app.
	// Check that the mounttable contains two endpoints, one of which is
	// v1EP2.
	endpoints = resolve(t, "appV1", 2)
	if endpoints[0] == v1EP2 {
		if endpoints[1] == v1EP2 {
			t.Fatalf("Both endpoints are the same")
		}
	} else if endpoints[1] != v1EP2 {
		t.Fatalf("Second endpoint should have been v1EP2: %v, %v", endpoints, v1EP2)
	}

	// Stop first instance.
	stopApp(t, appID, instance1ID)
	verifyAppWorkspace(t, root, appID, instance1ID)

	// Only second instance is still running.
	if want, got := v1EP2, resolve(t, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Start a third instance.
	instance3ID := startApp(t, appID)
	// Wait until the app pings us that it's ready.
	verifyHelperArgs(t, pingCh, userName(t))

	resolve(t, "appV2", 1)

	// Stop second instance.
	stopApp(t, appID, instance2ID)
	resolveExpectNotFound(t, "appV1")

	// Stop third instance.
	stopApp(t, appID, instance3ID)
	resolveExpectNotFound(t, "appV2")

	// Revert the app.
	revertApp(t, appID)

	// Start a fourth instance.  It should be started from version 1.
	instance4ID := startApp(t, appID)
	verifyHelperArgs(t, pingCh, userName(t)) // Wait until the app pings us that it's ready.
	resolve(t, "appV1", 1)
	stopApp(t, appID, instance4ID)
	resolveExpectNotFound(t, "appV1")

	// We are already on the first version, no further revert possible.
	revertAppExpectError(t, appID, naming.ErrNoSuchName.ID)

	// Uninstall the app.
	uninstallApp(t, appID)

	// Updating the installation should no longer be allowed.
	updateAppExpectError(t, appID, verror.BadArg)

	// Reverting the installation should no longer be allowed.
	revertAppExpectError(t, appID, verror.BadArg)

	// Starting new instances should no longer be allowed.
	startAppExpectError(t, appID, verror.BadArg)

	// Cleanly shut down the node manager.
	syscall.Kill(nmh.Pid(), syscall.SIGINT)
	nms.Expect("nm terminating")
	nms.ExpectEOF()
}

func newRuntime(t *testing.T) veyron2.Runtime {
	runtime, err := rt.New()
	if err != nil {
		t.Fatalf("rt.New() failed: %v", err)
	}
	runtime.Namespace().SetRoots(rt.R().Namespace().Roots()[0])
	return runtime
}

func tryInstall(rt veyron2.Runtime) error {
	appsName := "nm//apps"
	stub := node.ApplicationClient(appsName, rt.Client())
	if _, err := stub.Install(rt.NewContext(), mockApplicationRepoName); err != nil {
		return fmt.Errorf("Install failed: %v", err)
	}
	return nil
}

// TestNodeManagerClaim claims a nodemanager and tests ACL permissions on its methods.
func TestNodeManagerClaim(t *testing.T) {
	sh, deferFn := createShellAndMountTable(t)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	crDir, crEnv := credentialsForChild("nodemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	_, nms := runShellCommand(t, sh, crEnv, nodeManagerCmd, "nm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := readPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "trapp")

	nodeStub := node.NodeClient("nm//nm")
	selfRT := rt.R()
	otherRT := newRuntime(t)
	defer otherRT.Cleanup()

	// Nodemanager should have open ACLs before we claim it and so an Install from otherRT should succeed.
	if err := tryInstall(otherRT); err != nil {
		t.Fatal(err)
	}
	// Claim the nodemanager with selfRT as <defaultblessing>/mydevice
	if err := nodeStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "mydevice"}); err != nil {
		t.Fatal(err)
	}

	// Installation should succeed since rt.R() (a.k.a. selfRT) is now the
	// "owner" of the nodemanager.
	appID := installApp(t)

	// otherRT should be unable to install though, since the ACLs have changed now.
	if err := tryInstall(otherRT); err == nil {
		t.Fatalf("Install should have failed from otherRT")
	}

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	// Start an instance of the app.
	instanceID := startApp(t, appID)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}
	resolve(t, "trapp", 1)
	suspendApp(t, appID, instanceID)

	// TODO(gauthamt): Test that ACLs persist across nodemanager restarts
}

func TestNodeManagerUpdateACL(t *testing.T) {
	sh, deferFn := createShellAndMountTable(t)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	var (
		idp = tsecurity.NewIDProvider("root")
		// The two "processes"/runtimes which will act as IPC clients to the
		// nodemanager process.
		selfRT  = rt.R()
		otherRT = newRuntime(t)
	)
	defer otherRT.Cleanup()
	// By default, selfRT and otherRT will have blessings generated based on the
	// username/machine name running this process. Since these blessings will appear
	// in ACLs, give them recognizable names.
	if err := idp.Bless(selfRT.Principal(), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(otherRT.Principal(), "other"); err != nil {
		t.Fatal(err)
	}

	crDir, crEnv := credentialsForChild("nodemanager")
	defer os.RemoveAll(crDir)

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	_, nms := runShellCommand(t, sh, crEnv, nodeManagerCmd, "nm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := readPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create an envelope for an app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps")

	nodeStub := node.NodeClient("nm//nm")
	acl, etag, err := nodeStub.GetACL(selfRT.NewContext())
	if err != nil {
		t.Fatalf("GetACL failed:%v", err)
	}
	if etag != "default" {
		t.Fatalf("getACL expected:default, got:%v(%v)", etag, acl)
	}

	// Claim the nodemanager as "root/self/mydevice"
	if err := nodeStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "mydevice"}); err != nil {
		t.Fatal(err)
	}
	expectedACL := make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		expectedACL[string(tag)] = access.ACL{In: []security.BlessingPattern{"root/self/mydevice"}}
	}
	var b bytes.Buffer
	if err := expectedACL.WriteTo(&b); err != nil {
		t.Fatalf("Failed to save ACL:%v", err)
	}
	md5hash := md5.Sum(b.Bytes())
	expectedETAG := hex.EncodeToString(md5hash[:])
	if acl, etag, err = nodeStub.GetACL(selfRT.NewContext()); err != nil {
		t.Fatal(err)
	}
	if etag != expectedETAG {
		t.Fatalf("getACL expected:%v(%v), got:%v(%v)", expectedACL, expectedETAG, acl, etag)
	}
	// Install from otherRT should fail, since it does not match the ACL.
	if err := tryInstall(otherRT); err == nil {
		t.Fatalf("Install should have failed with random identity")
	}
	newACL := make(access.TaggedACLMap)
	for _, tag := range access.AllTypicalTags() {
		newACL.Add("root/other", string(tag))
	}
	if err := nodeStub.SetACL(selfRT.NewContext(), newACL, "invalid"); err == nil {
		t.Fatalf("SetACL should have failed with invalid etag")
	}
	if err := nodeStub.SetACL(selfRT.NewContext(), newACL, etag); err != nil {
		t.Fatal(err)
	}
	// Install should now fail with selfRT, which no longer matches the ACLs but succeed with otherRT, which does.
	if err := tryInstall(selfRT); err == nil {
		t.Errorf("Install should have failed with selfRT since it should no longer match the ACL")
	}
	if err := tryInstall(otherRT); err != nil {
		t.Error(err)
	}
}

// TestNodeManagerInstall verifies the 'self install' functionality of the node
// manager: it runs SelfInstall in a child process, then runs the executable
// from the soft link that the installation created.  This should bring up a
// functioning node manager.
func TestNodeManagerInstall(t *testing.T) {
	sh, deferFn := createShellAndMountTable(t)
	defer deferFn()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Current link does not have to live in the root dir, but it's
	// convenient to put it there so we have everything in one place.
	currLink := filepath.Join(root, "current_link")

	// Create an 'envelope' for the node manager that we can pass to the
	// installer, to ensure that the node manager that the installer
	// configures can run. The installer uses a shell script, so we need
	// to get a set of arguments that will work from within the shell
	// script in order for it to start a node manager.
	// We don't need the environment here since that can only
	// be correctly setup in the actual 'installer' command implementation
	// (in this case the shell script) which will inherit its environment
	// when we run it.
	// TODO(caprita): figure out if this is really necessary, hopefully not.
	nmargs, _ := sh.CommandEnvelope(nodeManagerCmd, nil)
	argsForNodeManager := append([]string{nodeManagerCmd, "--"}, nmargs[1:]...)
	argsForNodeManager = append(argsForNodeManager, "nm")

	// Add vars to instruct the installer how to configure the node manager.
	installerEnv := []string{config.RootEnv + "=" + root, config.CurrentLinkEnv + "=" + currLink, config.HelperEnv + "=" + "unused"}
	installerh, installers := runShellCommand(t, sh, installerEnv, installerCmd, argsForNodeManager...)
	installers.ExpectEOF()
	installerh.Shutdown(os.Stderr, os.Stderr)

	// CurrLink should now be pointing to a node manager script that
	// can start up a node manager.
	nmh, nms := runShellCommand(t, sh, nil, execScriptCmd, currLink)

	// We need the pid of the child process started by the node manager
	// script above to signal it, not the pid of the script itself.
	// TODO(caprita): the scripts that the node manager generates
	// should progagate signals so you don't have to obtain the pid of the
	// child by reading it from stdout as we do here. The node manager should
	// be able to retain a list of the processes it spawns and be confident
	// that sending a signal to them will also result in that signal being
	// sent to their children and so on.
	pid := readPID(t, nms)
	resolve(t, "nm", 1)
	revertNodeExpectError(t, "nm", naming.ErrNoSuchName.ID) // No previous version available.
	syscall.Kill(pid, syscall.SIGINT)

	nms.Expect("nm terminating")
	nms.ExpectEOF()
	nmh.Shutdown(os.Stderr, os.Stderr)
}

func TestNodeManagerGlobAndDebug(t *testing.T) {
	sh, deferFn := createShellAndMountTable(t)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	crDir, crEnv := credentialsForChild("nodemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	_, nms := runShellCommand(t, sh, crEnv, nodeManagerCmd, "nm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := readPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	// Create the envelope for the first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install the app.
	appID := installApp(t)
	installID := path.Base(appID)

	// Start an instance of the app.
	instance1ID := startApp(t, appID)

	// Wait until the app pings us that it's ready.
	select {
	case <-pingCh:
	case <-time.After(pingTimeout):
		t.Fatalf("failed to get ping")
	}

	testcases := []struct {
		name, pattern string
		expected      []string
	}{
		{"nm", "...", []string{
			"",
			"apps",
			"apps/google naps",
			"apps/google naps/" + installID,
			"apps/google naps/" + installID + "/" + instance1ID,
			"apps/google naps/" + installID + "/" + instance1ID + "/logs",
			"apps/google naps/" + installID + "/" + instance1ID + "/logs/STDERR-<timestamp>",
			"apps/google naps/" + installID + "/" + instance1ID + "/logs/STDOUT-<timestamp>",
			"apps/google naps/" + installID + "/" + instance1ID + "/logs/bin.INFO",
			"apps/google naps/" + installID + "/" + instance1ID + "/logs/bin.<*>.INFO.<timestamp>",
			"apps/google naps/" + installID + "/" + instance1ID + "/pprof",
			"apps/google naps/" + installID + "/" + instance1ID + "/stats",
			"apps/google naps/" + installID + "/" + instance1ID + "/stats/ipc",
			"apps/google naps/" + installID + "/" + instance1ID + "/stats/system",
			"apps/google naps/" + installID + "/" + instance1ID + "/stats/system/start-time-rfc1123",
			"apps/google naps/" + installID + "/" + instance1ID + "/stats/system/start-time-unix",
			"nm",
		}},
		{"nm/apps", "*", []string{"google naps"}},
		{"nm/apps/google naps", "*", []string{installID}},
		{"nm/apps/google naps/" + installID, "*", []string{instance1ID}},
		{"nm/apps/google naps/" + installID + "/" + instance1ID, "*", []string{"logs", "pprof", "stats"}},
		{"nm/apps/google naps/" + installID + "/" + instance1ID + "/logs", "*", []string{
			"STDERR-<timestamp>",
			"STDOUT-<timestamp>",
			"bin.INFO",
			"bin.<*>.INFO.<timestamp>",
		}},
		{"nm/apps/google naps/" + installID + "/" + instance1ID + "/stats/system", "start-time*", []string{"start-time-rfc1123", "start-time-unix"}},
	}
	logFileTimeStampRE := regexp.MustCompile("(STDOUT|STDERR)-[0-9]+$")
	logFileTrimInfoRE := regexp.MustCompile(`bin\..*\.INFO\.[0-9.-]+$`)
	logFileRemoveErrorFatalWarningRE := regexp.MustCompile("(ERROR|FATAL|WARNING)")
	statsTrimRE := regexp.MustCompile("/stats/(ipc|system(/start-time.*)?)$")
	for _, tc := range testcases {
		results, err := testutil.GlobName(tc.name, tc.pattern)
		if err != nil {
			t.Errorf("unexpected glob error for (%q, %q): %v", tc.name, tc.pattern, err)
			continue
		}
		filteredResults := []string{}
		for _, name := range results {
			// Keep only the stats object names that match this RE.
			if strings.Contains(name, "/stats/") && !statsTrimRE.MatchString(name) {
				continue
			}
			// Remove ERROR, WARNING, FATAL log files because
			// they're not consistently there.
			if logFileRemoveErrorFatalWarningRE.MatchString(name) {
				continue
			}
			name = logFileTimeStampRE.ReplaceAllString(name, "$1-<timestamp>")
			name = logFileTrimInfoRE.ReplaceAllString(name, "bin.<*>.INFO.<timestamp>")
			filteredResults = append(filteredResults, name)
		}
		sort.Strings(filteredResults)
		sort.Strings(tc.expected)
		if !reflect.DeepEqual(filteredResults, tc.expected) {
			t.Errorf("unexpected result for (%q, %q). Got %q, want %q", tc.name, tc.pattern, filteredResults, tc.expected)
		}
	}

	// Call Size() on the log file objects.
	files, err := testutil.GlobName("nm", "apps/google naps/"+installID+"/"+instance1ID+"/logs/*")
	if err != nil {
		t.Errorf("unexpected glob error: %v", err)
	}
	if want, got := 4, len(files); got < want {
		t.Errorf("Unexpected number of matches. Got %d, want at least %d", got, want)
	}
	for _, file := range files {
		name := naming.Join("nm", file)
		c := logreader.LogFileClient(name)
		if _, err := c.Size(rt.R().NewContext()); err != nil {
			t.Errorf("Size(%q) failed: %v", name, err)
		}
	}

	// Call Value() on some of the stats objects.
	objects, err := testutil.GlobName("nm", "apps/google naps/"+installID+"/"+instance1ID+"/stats/system/start-time*")
	if err != nil {
		t.Errorf("unexpected glob error: %v", err)
	}
	if want, got := 2, len(objects); got != want {
		t.Errorf("Unexpected number of matches. Got %d, want %d", got, want)
	}
	for _, obj := range objects {
		name := naming.Join("nm", obj)
		c := stats.StatsClient(name)
		if _, err := c.Value(rt.R().NewContext()); err != nil {
			t.Errorf("Value(%q) failed: %v", name, err)
		}
	}

	// Call CmdLine() on the pprof object.
	{
		name := "nm/apps/google naps/" + installID + "/" + instance1ID + "/pprof"
		c := pprof.PProfClient(name)
		v, err := c.CmdLine(rt.R().NewContext())
		if err != nil {
			t.Errorf("CmdLine(%q) failed: %v", name, err)
		}
		if len(v) == 0 {
			t.Fatalf("Unexpected empty cmdline: %v", v)
		}
		if got, want := filepath.Base(v[0]), "bin"; got != want {
			t.Errorf("Unexpected value for argv[0]. Got %v, want %v", got, want)
		}
	}
}

func listAndVerifyAssociations(t *testing.T, stub node.NodeClientMethods, run veyron2.Runtime, expected []node.Association) {
	assocs, err := stub.ListAssociations(run.NewContext())
	if err != nil {
		t.Fatalf("ListAssociations failed %v", err)
	}
	compareAssociations(t, assocs, expected)
}

// TODO(rjkroege): Verify that associations persist across restarts
// once permanent storage is added.
func TestAccountAssociation(t *testing.T) {
	sh, deferFn := createShellAndMountTable(t)
	defer deferFn()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	var (
		idp = tsecurity.NewIDProvider("root")
		// The two "processes"/runtimes which will act as IPC clients to
		// the nodemanager process.
		selfRT  = rt.R()
		otherRT = newRuntime(t)
	)
	defer otherRT.Cleanup()
	// By default, selfRT and otherRT will have blessings generated based
	// on the username/machine name running this process. Since these
	// blessings will appear in test expecations, give them readable
	// names.
	if err := idp.Bless(selfRT.Principal(), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(otherRT.Principal(), "other"); err != nil {
		t.Fatal(err)
	}
	crFile, crEnv := credentialsForChild("nodemanager")
	defer os.RemoveAll(crFile)

	_, nms := runShellCommand(t, sh, crEnv, nodeManagerCmd, "nm", root, "unused_helper", "unused_app_repo_name", "unused_curr_link")
	pid := readPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	nodeStub := node.NodeClient("nm//nm")

	// Attempt to list associations on the node manager without having
	// claimed it.
	if list, err := nodeStub.ListAssociations(otherRT.NewContext()); err != nil || list != nil {
		t.Fatalf("ListAssociations should fail on unclaimed node manager but did not: %v", err)
	}

	// self claims the node manager.
	if err := nodeStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "alice"}); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	vlog.VI(2).Info("Verify that associations start out empty.")
	listAndVerifyAssociations(t, nodeStub, selfRT, []node.Association(nil))

	if err := nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/self", "root/other"}, "alice_system_account"); err != nil {
		t.Fatalf("ListAssociations failed %v", err)
	}
	vlog.VI(2).Info("Added association should appear.")
	listAndVerifyAssociations(t, nodeStub, selfRT, []node.Association{
		{
			"root/self",
			"alice_system_account",
		},
		{
			"root/other",
			"alice_system_account",
		},
	})

	if err := nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/self", "root/other"}, "alice_other_account"); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	vlog.VI(2).Info("Change the associations and the change should appear.")
	listAndVerifyAssociations(t, nodeStub, selfRT, []node.Association{
		{
			"root/self",
			"alice_other_account",
		},
		{
			"root/other",
			"alice_other_account",
		},
	})

	if err := nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, ""); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	vlog.VI(2).Info("Verify that we can remove an association.")
	listAndVerifyAssociations(t, nodeStub, selfRT, []node.Association{
		{
			"root/self",
			"alice_other_account",
		},
	})
}

// userName is a helper function to determine the system name that the
// test is running under.
func userName(t *testing.T) string {
	u, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current() failed: %v", err)
	}
	return u.Username
}

func TestAppWithSuidHelper(t *testing.T) {
	sh, deferFn := createShellAndMountTable(t)
	defer deferFn()

	// Set up mock application and binary repositories.
	envelope, cleanup := startMockRepos(t)
	defer cleanup()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	var (
		idp = tsecurity.NewIDProvider("root")
		// The two "processes"/runtimes which will act as IPC clients to
		// the nodemanager process.
		selfRT  = rt.R()
		otherRT = newRuntime(t)
	)
	defer otherRT.Cleanup()

	// By default, selfRT and otherRT will have blessings generated
	// based on the username/machine name running this process. Since
	// these blessings can appear in debugging output, give them
	// recognizable names.
	if err := idp.Bless(selfRT.Principal(), "self"); err != nil {
		t.Fatal(err)
	}
	if err := idp.Bless(otherRT.Principal(), "other"); err != nil {
		t.Fatal(err)
	}

	crDir, crEnv := credentialsForChild("nodemanager")
	defer os.RemoveAll(crDir)

	// Create a script wrapping the test target that implements
	// suidhelper.
	helperPath := generateSuidHelperScript(t, root)

	_, nms := runShellCommand(t, sh, crEnv, nodeManagerCmd, "-mocksetuid", "nm", root, helperPath, "unused_app_repo_name", "unused_curr_link")
	pid := readPID(t, nms)
	defer syscall.Kill(pid, syscall.SIGINT)

	nodeStub := node.NodeClient("nm//nm")

	// Create the local server that the app uses to tell us which system name
	// the node manager wished to run it as.
	server, _ := newServer()
	defer server.Stop()
	pingCh := make(chan string, 1)
	if err := server.Serve("pingserver", pingServer(pingCh), nil); err != nil {
		t.Fatalf("Serve(%q, <dispatcher>) failed: %v", "pingserver", err)
	}

	// Create an envelope for a first version of the app.
	*envelope = envelopeFromShell(sh, nil, appCmd, "google naps", "appV1")

	// Install and start the app as root/self.
	appID := installApp(t, selfRT)

	// Claim the nodemanager with selfRT as root/self/alice
	if err := nodeStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "alice"}); err != nil {
		t.Fatal(err)
	}

	// Start an instance of the app but this time it should fail: we do
	// not have an associated uname for the invoking identity.
	startAppExpectError(t, appID, verror.NoAccess, selfRT)

	// Create an association for selfRT
	if err := nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/self"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	instance1ID := startApp(t, appID, selfRT)
	verifyHelperArgs(t, pingCh, testUserName) // Wait until the app pings us that it's ready.
	stopApp(t, appID, instance1ID, selfRT)

	vlog.VI(2).Infof("other attempting to run an app without access. Should fail.")
	startAppExpectError(t, appID, verror.NoAccess, otherRT)

	// Self will now let other also install apps.
	if err := nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	// Add Start to the ACL list for root/other.
	newACL, _, err := nodeStub.GetACL(selfRT.NewContext())
	if err != nil {
		t.Fatalf("GetACL failed %v", err)
	}
	newACL.Add("root/other", string(access.Write))
	if err := nodeStub.SetACL(selfRT.NewContext(), newACL, ""); err != nil {
		t.Fatalf("SetACL failed %v", err)
	}

	// With the introduction of per installation and per instance ACLs, while other now
	// has administrator permissions on the node manager, other doesn't have execution
	// permissions for the app. So this will fail.
	vlog.VI(2).Infof("other attempting to run an app still without access. Should fail.")
	startAppExpectError(t, appID, verror.NoAccess, otherRT)

	// But self can give other permissions  to start applications.
	vlog.VI(2).Infof("self attempting to give other permission to start %s", appID)
	newACL, _, err = appStub(appID).GetACL(selfRT.NewContext())
	if err != nil {
		t.Fatalf("GetACL on appID: %v failed %v", appID, err)
	}
	newACL.Add("root/other", string(access.Read))
	if err = appStub(appID).SetACL(selfRT.NewContext(), newACL, ""); err != nil {
		t.Fatalf("SetACL on appID: %v failed: %v", appID, err)
	}

	vlog.VI(2).Infof("other attempting to run an app with access. Should succeed.")
	instance2ID := startApp(t, appID, otherRT)
	verifyHelperArgs(t, pingCh, testUserName) // Wait until the app pings us that it's ready.
	suspendApp(t, appID, instance2ID, otherRT)

	vlog.VI(2).Infof("Verify that Resume with the same systemName works.")
	resumeApp(t, appID, instance2ID, otherRT)
	verifyHelperArgs(t, pingCh, testUserName) // Wait until the app pings us that it's ready.
	suspendApp(t, appID, instance2ID, otherRT)

	vlog.VI(2).Infof("Verify that other can install and run applications.")
	otherAppID := installApp(t, otherRT)

	vlog.VI(2).Infof("other attempting to run an app that other installed. Should succeed.")
	instance4ID := startApp(t, otherAppID, otherRT)
	verifyHelperArgs(t, pingCh, testUserName) // Wait until the app pings us that it's ready.

	// Clean up.
	stopApp(t, otherAppID, instance4ID, otherRT)

	// Change the associated system name.
	if err := nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, anotherTestUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	vlog.VI(2).Infof("Show that Resume with a different systemName fails.")
	resumeAppExpectError(t, appID, instance2ID, verror.NoAccess, otherRT)

	// Clean up.
	stopApp(t, appID, instance2ID, otherRT)

	vlog.VI(2).Infof("Show that Start with different systemName works.")
	instance3ID := startApp(t, appID, otherRT)
	verifyHelperArgs(t, pingCh, anotherTestUserName) // Wait until the app pings us that it's ready.

	// Clean up.
	stopApp(t, appID, instance3ID, otherRT)
}

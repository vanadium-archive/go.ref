package impl_test

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	goexec "os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"veyron.io/veyron/veyron/lib/exec"
	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/lib/testutil/blackbox"
	tsecurity "veyron.io/veyron/veyron/lib/testutil/security"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/services/mgmt/node/config"
	"veyron.io/veyron/veyron/services/mgmt/node/impl"
	suidhelper "veyron.io/veyron/veyron/services/mgmt/suidhelper/impl"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/services/mgmt/node"
	"veyron.io/veyron/veyron2/services/mounttable"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

// TestHelperProcess is blackbox boilerplate.
func TestHelperProcess(t *testing.T) {
	// All TestHelperProcess invocations need a Runtime. Create it here.
	rt.Init()

	// Disable the cache because we will be manipulating/using the namespace
	// across multiple processes and want predictable behaviour without
	// relying on timeouts.
	rt.R().Namespace().CacheCtl(naming.DisableCache(true))

	blackbox.CommandTable["execScript"] = execScript
	blackbox.CommandTable["nodeManager"] = nodeManager
	blackbox.CommandTable["app"] = app

	blackbox.HelperProcess(t)
}

func init() {
	if os.Getenv("VEYRON_BLACKBOX_TEST") == "1" {
		return
	}

	// All the tests require a runtime; so just create it here.
	rt.Init()

	// Disable the cache because we will be manipulating/using the namespace
	// across multiple processes and want predictable behaviour without
	// relying on timeouts.
	rt.R().Namespace().CacheCtl(naming.DisableCache(true))
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
func execScript(args []string) {
	if want, got := 1, len(args); want != got {
		vlog.Fatalf("execScript expected %d arguments, got %d instead", want, got)
	}
	script := args[0]
	env := []string{}
	if os.Getenv("PAUSE_BEFORE_STOP") == "1" {
		env = append(env, "PAUSE_BEFORE_STOP=1")
	}
	cmd := goexec.Cmd{
		Path:   script,
		Env:    env,
		Stdin:  os.Stdin,
		Stderr: os.Stderr,
		Stdout: os.Stdout,
	}
	if err := cmd.Run(); err != nil {
		vlog.Fatalf("Run cmd %v failed: %v", cmd, err)
	}
}

// nodeManager sets up a node manager server.  It accepts the name to publish
// the server under as an argument.  Additional arguments can optionally specify
// node manager config settings.
func nodeManager(args []string) {
	if len(args) == 0 {
		vlog.Fatalf("nodeManager expected at least an argument")
	}
	publishName := args[0]
	args = args[1:]

	defer fmt.Printf("%v terminating\n", publishName)
	defer rt.R().Cleanup()
	server, endpoint := newServer()
	defer server.Stop()
	name := naming.MakeTerminal(naming.JoinAddressName(endpoint, ""))
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
		if want, got := 3, len(args); want != got {
			vlog.Fatalf("expected %d additional arguments, got %d instead", want, got)
		}
		configState.Root, configState.Origin, configState.CurrentLink = args[0], args[1], args[2]
	}

	dispatcher, err := impl.NewDispatcher(configState)
	if err != nil {
		vlog.Fatalf("Failed to create node manager dispatcher: %v", err)
	}
	if err := server.Serve(publishName, dispatcher); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}

	impl.InvokeCallback(name)
	fmt.Printf("ready:%d\n", os.Getpid())

	<-signals.ShutdownOnSignals()
	if os.Getenv("PAUSE_BEFORE_STOP") == "1" {
		blackbox.WaitForEOFOnStdin()
	}
	if dispatcher.Leaking() {
		vlog.Fatalf("node manager leaking resources")
	}
}

// appService defines a test service that the test app should be running.
// TODO(caprita): Use this to make calls to the app and verify how Suspend/Stop
// interact with an active service.
type appService struct{}

func (appService) Echo(_ ipc.ServerCall, message string) (string, error) {
	return message, nil
}

func ping() {
	if call, err := rt.R().Client().StartCall(rt.R().NewContext(), "pingserver", "Ping", []interface{}{os.Getenv(suidhelper.SavedArgs)}); err != nil {
		vlog.Fatalf("StartCall failed: %v", err)
	} else if err = call.Finish(); err != nil {
		vlog.Fatalf("Finish failed: %v", err)
	}
}

func app(args []string) {
	if expected, got := 1, len(args); expected != got {
		vlog.Fatalf("Unexpected number of arguments: expected %d, got %d", expected, got)
	}
	publishName := args[0]

	defer rt.R().Cleanup()
	server, _ := newServer()
	defer server.Stop()
	if err := server.Serve(publishName, ipc.LeafDispatcher(new(appService), nil)); err != nil {
		vlog.Fatalf("Serve(%v) failed: %v", publishName, err)
	}
	ping()
	<-signals.ShutdownOnSignals()
	if err := ioutil.WriteFile("testfile", []byte("goodbye world"), 0600); err != nil {
		vlog.Fatalf("Failed to write testfile: %v", err)
	}
}

// TODO(rjkroege): generateNodeManagerScript and generateSuidHelperScript have code
// similarity that might benefit from refactoring.
// generateNodeManagerScript is very similar in behavior to generateScript in node_invoker.go.
// However, we chose to re-implement it here for two reasons: (1) avoid making
// generateScript public; and (2) how the test choses to invoke the node manager
// subprocess the first time should be independent of how node manager
// implementation sets up its updated versions.
func generateNodeManagerScript(t *testing.T, root string, cmd *goexec.Cmd) string {
	output := "#!/bin/bash\n"
	output += strings.Join(config.QuoteEnv(cmd.Env), " ") + " "
	output += cmd.Args[0] + " " + strings.Join(cmd.Args[1:], " ")
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

// generateSuidHelperScript builds a script to execute the test target as
// a suidhelper instance and installs it in <root>/helper.
func generateSuidHelperScript(t *testing.T, root string) {
	output := "#!/bin/bash\n"
	output += "VEYRON_SUIDHELPER_TEST=1"
	output += " "
	output += "exec" + " " + os.Args[0] + " -test.run=TestSuidHelper $*"
	output += "\n"

	vlog.VI(1).Infof("script\n%s", output)

	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	path := filepath.Join(root, "helper")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		t.Fatalf("WriteFile(%v) failed: %v", path, err)
	}
}

// nodeEnvelopeFromCmd returns a node manager application envelope that
// describes the given command object.
func nodeEnvelopeFromCmd(cmd *goexec.Cmd) *application.Envelope {
	return envelopeFromCmd(application.NodeManagerTitle, cmd)
}

// envelopeFromCmd returns an envelope that describes the given command object.
func envelopeFromCmd(title string, cmd *goexec.Cmd) *application.Envelope {
	return &application.Envelope{
		Title:  title,
		Args:   cmd.Args[1:],
		Env:    cmd.Env,
		Binary: "br",
	}
}

// setupRootDir sets up and returns the local filesystem location that the node
// manager is told to use, as well as a cleanup function.
func setupRootDir(t *testing.T) (string, func()) {
	rootDir, err := ioutil.TempDir("", "nodemanager")
	if err != nil {
		t.Fatalf("Failed to set up temporary dir for test: %v", err)
	}
	// On some operating systems (e.g. darwin) os.TempDir() can return a
	// symlink. To avoid having to account for this eventuality later,
	// evaluate the symlink.
	rootDir, err = filepath.EvalSymlinks(rootDir)
	if err != nil {
		vlog.Fatalf("EvalSymlinks(%v) failed: %v", rootDir, err)
	}
	return rootDir, func() {
		if t.Failed() {
			t.Logf("You can examine the node manager workspace at %v", rootDir)
		} else {
			os.RemoveAll(rootDir)
		}
	}
}

// readPID waits for the "ready:<PID>" line from the child and parses out the
// PID of the child.
func readPID(t *testing.T, c *blackbox.Child) int {
	line, err := c.ReadLineFromChild()
	if err != nil {
		t.Fatalf("ReadLineFromChild() failed: %v", err)
		return 0
	}
	colon := strings.LastIndex(line, ":")
	if colon == -1 {
		t.Fatalf("LastIndex(%q, %q) returned -1", line, ":")
		return 0
	}
	pid, err := strconv.Atoi(line[colon+1:])
	if err != nil {
		t.Fatalf("Atoi(%q) failed: %v", line[colon+1:], err)
	}
	return pid
}

// TestNodeManagerUpdateAndRevert makes the node manager go through the motions of updating
// itself to newer versions (twice), and reverting itself back (twice).  It also
// checks that update and revert fail when they're supposed to.  The initial
// node manager is started 'by hand' via a blackbox command.  Further versions
// are started through the soft link that the node manager itself updates.
func TestNodeManagerUpdateAndRevert(t *testing.T) {
	// Set up mount table, application, and binary repositories.
	defer setupLocalNamespace(t)()
	envelope, cleanup := startApplicationRepository()
	defer cleanup()
	defer startBinaryRepository()()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Current link does not have to live in the root dir.
	currLink := filepath.Join(os.TempDir(), "testcurrent")
	os.Remove(currLink) // Start out with a clean slate.
	defer os.Remove(currLink)

	// Set up the initial version of the node manager, the so-called
	// "factory" version.
	nm := blackbox.HelperCommand(t, "nodeManager", "factoryNM", root, "ar", currLink)
	defer setupChildCommand(nm)()

	// This is the script that we'll point the current link to initially.
	scriptPathFactory := generateNodeManagerScript(t, root, nm.Cmd)

	if err := os.Symlink(scriptPathFactory, currLink); err != nil {
		t.Fatalf("Symlink(%q, %q) failed: %v", scriptPathFactory, currLink, err)
	}
	// We instruct the initial node manager that we run to pause before
	// stopping its service, so that we get a chance to verify that
	// attempting an update while another one is ongoing will fail.
	nm.Cmd.Env = exec.Setenv(nm.Cmd.Env, "PAUSE_BEFORE_STOP", "1")

	resolveExpectNotFound(t, "factoryNM") // Ensure a clean slate.

	// Start the node manager -- we use the blackbox-generated command to
	// start it.  We could have also used the scriptPathFactory to start it, but
	// this demonstrates that the initial node manager could be started by
	// hand as long as the right initial configuration is passed into the
	// node manager implementation.
	if err := nm.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer := nm.Cleanup
	defer func() {
		if deferrer != nil {
			deferrer()
		}
	}()
	readPID(t, nm)
	resolve(t, "factoryNM", 1) // Verify the node manager has published itself.

	// Simulate an invalid envelope in the application repository.
	*envelope = *nodeEnvelopeFromCmd(nm.Cmd)
	envelope.Title = "bogus"
	updateNodeExpectError(t, "factoryNM", verror.BadArg)  // Incorrect title.
	revertNodeExpectError(t, "factoryNM", verror.NoExist) // No previous version available.

	// Set up a second version of the node manager.  We use the blackbox
	// command solely to collect the args and env we need to provide the
	// application repository with an envelope that will actually run the
	// node manager subcommand.  The blackbox command is never started by
	// hand -- instead, the information in the envelope will be used by the
	// node manager to stage the next version.
	nmV2 := blackbox.HelperCommand(t, "nodeManager", "v2NM")
	defer setupChildCommand(nmV2)()
	*envelope = *nodeEnvelopeFromCmd(nmV2.Cmd)
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

	// This is from the child node manager started by the node manager
	// as an update test.
	readPID(t, nm)
	nm.Expect("v2NM terminating")

	updateNodeExpectError(t, "factoryNM", verror.Exists) // Update already in progress.

	nm.CloseStdin()
	nm.Expect("factoryNM terminating")
	deferrer = nil
	nm.Cleanup()

	// A successful update means the node manager has stopped itself.  We
	// relaunch it from the current link.
	runNM := blackbox.HelperCommand(t, "execScript", currLink)
	resolveExpectNotFound(t, "v2NM") // Ensure a clean slate.
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer = runNM.Cleanup
	readPID(t, runNM)
	resolve(t, "v2NM", 1) // Current link should have been launching v2.

	// Try issuing an update without changing the envelope in the application
	// repository: this should fail, and current link should be unchanged.
	updateNodeExpectError(t, "v2NM", verror.NoExist)
	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Create a third version of the node manager and issue an update.
	nmV3 := blackbox.HelperCommand(t, "nodeManager", "v3NM")
	defer setupChildCommand(nmV3)()
	*envelope = *nodeEnvelopeFromCmd(nmV3.Cmd)
	updateNode(t, "v2NM")

	scriptPathV3 := evalLink()
	if scriptPathV3 == scriptPathV2 {
		t.Fatalf("current link didn't change")
	}

	// This is from the child node manager started by the node manager
	// as an update test.
	readPID(t, runNM)
	// Both the parent and child node manager should terminate upon successful
	// update.
	runNM.ExpectSet([]string{"v3NM terminating", "v2NM terminating"})

	deferrer = nil
	runNM.Cleanup()

	// Re-lanuch the node manager from current link.
	runNM = blackbox.HelperCommand(t, "execScript", currLink)
	// We instruct the node manager to pause before stopping its server, so
	// that we can verify that a second revert fails while a revert is in
	// progress.
	runNM.Cmd.Env = exec.Setenv(nm.Cmd.Env, "PAUSE_BEFORE_STOP", "1")
	resolveExpectNotFound(t, "v3NM") // Ensure a clean slate.
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer = runNM.Cleanup
	readPID(t, runNM)
	resolve(t, "v3NM", 1) // Current link should have been launching v3.

	// Revert the node manager to its previous version (v2).
	revertNode(t, "v3NM")
	revertNodeExpectError(t, "v3NM", verror.Exists) // Revert already in progress.
	runNM.CloseStdin()
	runNM.Expect("v3NM terminating")
	if evalLink() != scriptPathV2 {
		t.Fatalf("current link was not reverted correctly")
	}
	deferrer = nil
	runNM.Cleanup()

	// Re-launch the node manager from current link.
	runNM = blackbox.HelperCommand(t, "execScript", currLink)
	resolveExpectNotFound(t, "v2NM") // Ensure a clean slate.
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer = runNM.Cleanup
	readPID(t, runNM)
	resolve(t, "v2NM", 1) // Current link should have been launching v2.

	// Revert the node manager to its previous version (factory).
	revertNode(t, "v2NM")
	runNM.Expect("v2NM terminating")
	if evalLink() != scriptPathFactory {
		t.Fatalf("current link was not reverted correctly")
	}
	deferrer = nil
	runNM.Cleanup()

	// Re-launch the node manager from current link.
	runNM = blackbox.HelperCommand(t, "execScript", currLink)
	resolveExpectNotFound(t, "factoryNM") // Ensure a clean slate.
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer = runNM.Cleanup
	pid := readPID(t, runNM)
	resolve(t, "factoryNM", 1) // Current link should have been launching factory version.
	syscall.Kill(pid, syscall.SIGINT)
	runNM.Expect("factoryNM terminating")
	runNM.ExpectEOFAndWait()
}

type pingServerDisp chan<- string

func (p pingServerDisp) Ping(_ ipc.ServerCall, arg string) {
	p <- arg
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
func verifyHelperArgs(t *testing.T, env, username string) {
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
	// Set up mount table, application, and binary repositories.
	defer setupLocalNamespace(t)()
	envelope, cleanup := startApplicationRepository()
	defer cleanup()
	defer startBinaryRepository()()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Create a script wrapping the test target that implements suidhelper.
	generateSuidHelperScript(t, root)

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	nm := blackbox.HelperCommand(t, "nodeManager", "nm", root, "unused app repo name", "unused curr link")
	defer setupChildCommand(nm)()
	if err := nm.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer nm.Cleanup()
	readPID(t, nm)

	// Create the local server that the app uses to let us know it's ready.
	server, _ := newServer()
	defer server.Stop()
	pingCh := make(chan string, 1)
	if err := server.Serve("pingserver", ipc.LeafDispatcher(pingServerDisp(pingCh), nil)); err != nil {
		t.Fatalf("Serve(%q, <dispatcher>) failed: %v", "pingserver", err)
	}

	// Create an envelope for a first version of the app.
	app := blackbox.HelperCommand(t, "app", "appV1")
	defer setupChildCommand(app)()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	// Install the app.
	appID := installApp(t)

	// Start an instance of the app.
	instance1ID := startApp(t, appID)

	u, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current() failed: %v", err)
	}
	verifyHelperArgs(t, <-pingCh, u.Username) // Wait until the app pings us that it's ready.

	v1EP1 := resolve(t, "appV1", 1)[0]

	// Suspend the app instance.
	suspendApp(t, appID, instance1ID)
	resolveExpectNotFound(t, "appV1")

	resumeApp(t, appID, instance1ID)
	verifyHelperArgs(t, <-pingCh, u.Username) // Wait until the app pings us that it's ready.
	oldV1EP1 := v1EP1
	if v1EP1 = resolve(t, "appV1", 1)[0]; v1EP1 == oldV1EP1 {
		t.Fatalf("Expected a new endpoint for the app after suspend/resume")
	}

	// Start a second instance.
	instance2ID := startApp(t, appID)
	verifyHelperArgs(t, <-pingCh, u.Username) // Wait until the app pings us that it's ready.

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
	updateAppExpectError(t, appID, verror.NoExist)

	// Updating the installation should not work with a mismatched title.
	*envelope = *envelopeFromCmd("bogus", app.Cmd)
	updateAppExpectError(t, appID, verror.BadArg)

	// Create a second version of the app and update the app to it.
	app = blackbox.HelperCommand(t, "app", "appV2")
	defer setupChildCommand(app)()
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)
	updateApp(t, appID)

	// Second instance should still be running.
	if want, got := v1EP2, resolve(t, "appV1", 1)[0]; want != got {
		t.Fatalf("Resolve(%v): want: %v, got %v", "appV1", want, got)
	}

	// Resume first instance.
	resumeApp(t, appID, instance1ID)
	verifyHelperArgs(t, <-pingCh, u.Username) // Wait until the app pings us that it's ready.
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
	verifyHelperArgs(t, <-pingCh, u.Username) // Wait until the app pings us that it's ready.
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
	verifyHelperArgs(t, <-pingCh, u.Username) // Wait until the app pings us that it's ready.
	resolve(t, "appV1", 1)
	stopApp(t, appID, instance4ID)
	resolveExpectNotFound(t, "appV1")

	// We are already on the first version, no further revert possible.
	revertAppExpectError(t, appID, verror.NoExist)

	// Uninstall the app.
	uninstallApp(t, appID)

	// Updating the installation should no longer be allowed.
	updateAppExpectError(t, appID, verror.BadArg)

	// Reverting the installation should no longer be allowed.
	revertAppExpectError(t, appID, verror.BadArg)

	// Starting new instances should no longer be allowed.
	startAppExpectError(t, appID, verror.BadArg)

	// Cleanly shut down the node manager.
	syscall.Kill(nm.Cmd.Process.Pid, syscall.SIGINT)
	nm.Expect("nm terminating")
	nm.ExpectEOFAndWait()
}

type granter struct {
	ipc.CallOpt
	self     security.PrivateID
	blessing security.PublicID
}

func (g *granter) Grant(id security.PublicID) (security.PublicID, error) {
	var err error
	g.blessing, err = g.self.Bless(id, "claimernode", 10*time.Minute, nil)
	return g.blessing, err
}

func newRuntimeClient(t *testing.T, id security.PrivateID) (veyron2.Runtime, ipc.Client) {
	runtime, err := rt.New(veyron2.RuntimeID(id))
	if err != nil {
		t.Fatalf("rt.New() failed: %v", err)
	}
	runtime.Namespace().SetRoots(rt.R().Namespace().Roots()[0])
	nodeClient, err := runtime.NewClient()
	if err != nil {
		t.Fatalf("rt.NewClient() failed %v", err)
	}
	return runtime, nodeClient
}

func tryInstall(rt veyron2.Runtime, c ipc.Client) error {
	appsName := "nm//apps"
	stub, err := node.BindApplication(appsName, c)
	if err != nil {
		return fmt.Errorf("BindApplication(%v) failed: %v", appsName, err)
	}
	if _, err = stub.Install(rt.NewContext(), "ar"); err != nil {
		return fmt.Errorf("Install failed: %v", err)
	}
	return nil
}

// TestNodeManagerClaim claims a nodemanager and tests ACL permissions on its methods.
func TestNodeManagerClaim(t *testing.T) {
	// Set up mount table, application, and binary repositories.
	defer setupLocalNamespace(t)()
	envelope, cleanup := startApplicationRepository()
	defer cleanup()
	defer startBinaryRepository()()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	nm := blackbox.HelperCommand(t, "nodeManager", "nm", root, "unused app repo name", "unused curr link")
	defer setupChildCommand(nm)()
	if err := nm.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer nm.Cleanup()
	readPID(t, nm)

	// Create an envelope for an app.
	app := blackbox.HelperCommand(t, "app", "")
	defer setupChildCommand(app)()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	nodeStub, err := node.BindNode("nm//nm")
	if err != nil {
		t.Fatalf("BindNode failed: %v", err)
	}

	// Create a new identity and runtime.
	claimerIdentity := tsecurity.NewBlessedIdentity(rt.R().Identity(), "claimer")
	newRT, nodeClient := newRuntimeClient(t, claimerIdentity)
	defer newRT.Cleanup()

	// Nodemanager should have open ACLs before we claim it and so an Install
	// should succeed.
	if err = tryInstall(newRT, nodeClient); err != nil {
		t.Fatalf("%v", err)
	}
	// Claim the nodemanager with this identity.
	if err = nodeStub.Claim(rt.R().NewContext(), &granter{self: claimerIdentity}); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if err = tryInstall(newRT, nodeClient); err != nil {
		t.Fatalf("%v", err)
	}
	// Try to install with a new identity. This should fail.
	randomIdentity := tsecurity.NewBlessedIdentity(rt.R().Identity(), "random")
	newRT, nodeClient = newRuntimeClient(t, randomIdentity)
	defer newRT.Cleanup()
	if err = tryInstall(newRT, nodeClient); err == nil {
		t.Fatalf("Install should have failed with random identity")
	}
	// Try to install with the original identity. This should still work as the original identity
	// name is a prefix of the identity used by newRT.
	nodeClient, err = rt.R().NewClient()
	if err != nil {
		t.Fatalf("rt.NewClient() failed %v", err)
	}
	if err = tryInstall(rt.R(), nodeClient); err != nil {
		t.Fatalf("%v", err)
	}
	// TODO(gauthamt): Test that ACLs persist across nodemanager restarts
}

func TestNodeManagerUpdateACL(t *testing.T) {
	// Set up mount table, application, and binary repositories.
	defer setupLocalNamespace(t)()
	envelope, cleanup := startApplicationRepository()
	defer cleanup()
	defer startBinaryRepository()()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	nm := blackbox.HelperCommand(t, "nodeManager", "nm", root, "unused app repo name", "unused curr link")
	defer setupChildCommand(nm)()
	if err := nm.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer nm.Cleanup()
	readPID(t, nm)

	// Create an envelope for an app.
	app := blackbox.HelperCommand(t, "app", "")
	defer setupChildCommand(app)()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	nodeStub, err := node.BindNode("nm//nm")
	if err != nil {
		t.Fatalf("BindNode failed: %v", err)
	}
	acl, etag, err := nodeStub.GetACL(rt.R().NewContext())
	if err != nil {
		t.Fatalf("GetACL failed:%v", err)
	}
	if etag != "default" {
		t.Fatalf("getACL expected:default, got:%v(%v)", etag, acl)
	}

	// Create a new identity and claim the node manager
	claimerIdentity := tsecurity.NewBlessedIdentity(rt.R().Identity(), "claimer")
	grant := &granter{self: claimerIdentity}
	if err = nodeStub.Claim(rt.R().NewContext(), grant); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	expectedACL := security.ACL{In: make(map[security.BlessingPattern]security.LabelSet)}
	for _, name := range grant.blessing.Names() {
		expectedACL.In[security.BlessingPattern(name)] = security.AllLabels
	}
	var b bytes.Buffer
	if err := vsecurity.SaveACL(&b, expectedACL); err != nil {
		t.Fatalf("Failed to saveACL:%v", err)
	}
	md5hash := md5.Sum(b.Bytes())
	expectedETAG := hex.EncodeToString(md5hash[:])
	acl, etag, err = nodeStub.GetACL(rt.R().NewContext())
	if err != nil {
		t.Fatalf("GetACL failed")
	}
	if etag != expectedETAG {
		t.Fatalf("getACL expected:%v(%v), got:%v(%v)", expectedACL, expectedETAG, acl, etag)
	}
	// Try to install with a new identity. This should fail.
	randomIdentity := tsecurity.NewBlessedIdentity(rt.R().Identity(), "random")
	newRT, nodeClient := newRuntimeClient(t, randomIdentity)
	defer newRT.Cleanup()
	if err = tryInstall(newRT, nodeClient); err == nil {
		t.Fatalf("Install should have failed with random identity")
	}
	newACL := security.ACL{In: make(map[security.BlessingPattern]security.LabelSet)}
	for _, name := range randomIdentity.PublicID().Names() {
		newACL.In[security.BlessingPattern(name)] = security.AllLabels
	}
	// SetACL with invalid etag
	if err = nodeStub.SetACL(rt.R().NewContext(), newACL, "invalid"); err == nil {
		t.Fatalf("SetACL should have failed with invalid etag")
	}
	if err = nodeStub.SetACL(rt.R().NewContext(), newACL, etag); err != nil {
		t.Fatalf("SetACL failed:%v", err)
	}
	if err = tryInstall(newRT, nodeClient); err != nil {
		t.Fatalf("Install failed with new identity:%v", err)
	}
	// Try to install with the claimer identity. This should fail as the ACLs
	// belong to the random identity
	newRT, nodeClient = newRuntimeClient(t, claimerIdentity)
	defer newRT.Cleanup()
	if err = tryInstall(newRT, nodeClient); err == nil {
		t.Fatalf("Install should have failed with claimer identity")
	}
}

func TestNodeManagerGlob(t *testing.T) {
	// Set up mount table.
	defer setupLocalNamespace(t)()
	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	nm := blackbox.HelperCommand(t, "nodeManager", "nm", root, "unused app repo name", "unused curr link")
	defer setupChildCommand(nm)()
	if err := nm.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer nm.Cleanup()
	readPID(t, nm)

	c, err := mounttable.BindGlobbable("nm")
	if err != nil {
		t.Fatalf("BindGlobbable failed: %v", err)
	}

	stream, err := c.Glob(rt.R().NewContext(), "...")
	if err != nil {
		t.Errorf("Glob failed: %v", err)
	}
	results := []string{}
	iterator := stream.RecvStream()
	for iterator.Advance() {
		results = append(results, iterator.Value().Name)
	}
	sort.Strings(results)
	expected := []string{
		"",
		"apps",
		"nm",
	}
	if !reflect.DeepEqual(results, expected) {
		t.Errorf("unexpected result. Got %v, want %v", results, expected)
	}
	if err := iterator.Err(); err != nil {
		t.Errorf("unexpected stream error: %v", err)
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Finish failed: %v", err)
	}
}

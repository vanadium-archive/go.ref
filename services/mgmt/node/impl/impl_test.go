// TODO(caprita): This file is becoming unmanageable; split into several test
// files.

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
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"

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
	"veyron.io/veyron/veyron2/options"
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
	rt.Init(options.ForceNewSecurityModel{})

	// Disable the cache because we will be manipulating/using the namespace
	// across multiple processes and want predictable behaviour without
	// relying on timeouts.
	rt.R().Namespace().CacheCtl(naming.DisableCache(true))

	blackbox.CommandTable["execScript"] = execScript
	blackbox.CommandTable["nodeManager"] = nodeManager
	blackbox.CommandTable["app"] = app
	blackbox.CommandTable["installer"] = install

	blackbox.HelperProcess(t)
}

func init() {
	if os.Getenv("VEYRON_BLACKBOX_TEST") == "1" {
		return
	}

	// All the tests require a runtime; so just create it here.
	rt.Init(options.ForceNewSecurityModel{})

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
	output += "exec" + " " + os.Args[0] + " " + "-minuid=1" + " " + "-test.run=TestSuidHelper $*"
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

	// Current link does not have to live in the root dir, but it's
	// convenient to put it there so we have everything in one place.
	currLink := filepath.Join(root, "current_link")

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

// setupPingServer creates a server listening for a ping from a child app; it
// returns a channel on which the app's ping message is returned, and a cleanup
// function.
func setupPingServer(t *testing.T) (<-chan string, func()) {
	server, _ := newServer()
	pingCh := make(chan string, 1)
	if err := server.Serve("pingserver", ipc.LeafDispatcher(pingServerDisp(pingCh), nil)); err != nil {
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
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	// Create an envelope for a first version of the app.
	app := blackbox.HelperCommand(t, "app", "appV1")
	defer setupChildCommand(app)()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	// Install the app.
	appID := installApp(t)

	// Start an instance of the app.
	instance1ID := startApp(t, appID)

	verifyHelperArgs(t, <-pingCh, userName(t)) // Wait until the app pings us that it's ready.

	v1EP1 := resolve(t, "appV1", 1)[0]

	// Suspend the app instance.
	suspendApp(t, appID, instance1ID)
	resolveExpectNotFound(t, "appV1")

	resumeApp(t, appID, instance1ID)
	verifyHelperArgs(t, <-pingCh, userName(t)) // Wait until the app pings us that it's ready.
	oldV1EP1 := v1EP1
	if v1EP1 = resolve(t, "appV1", 1)[0]; v1EP1 == oldV1EP1 {
		t.Fatalf("Expected a new endpoint for the app after suspend/resume")
	}

	// Start a second instance.
	instance2ID := startApp(t, appID)
	verifyHelperArgs(t, <-pingCh, userName(t)) // Wait until the app pings us that it's ready.

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
	verifyHelperArgs(t, <-pingCh, userName(t)) // Wait until the app pings us that it's ready.
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
	verifyHelperArgs(t, <-pingCh, userName(t)) // Wait until the app pings us that it's ready.
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
	verifyHelperArgs(t, <-pingCh, userName(t)) // Wait until the app pings us that it's ready.
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
	p         security.Principal
	extension string
}

func (g *granter) Grant(other security.Blessings) (security.Blessings, error) {
	return g.p.Bless(other.PublicKey(), g.p.BlessingStore().Default(), g.extension, security.UnconstrainedUse())
}

func newRuntime(t *testing.T) veyron2.Runtime {
	runtime, err := rt.New(options.ForceNewSecurityModel{})
	if err != nil {
		t.Fatalf("rt.New() failed: %v", err)
	}
	runtime.Namespace().SetRoots(rt.R().Namespace().Roots()[0])
	return runtime
}

func tryInstall(rt veyron2.Runtime) error {
	appsName := "nm//apps"
	stub, err := node.BindApplication(appsName, rt.Client())
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
	app := blackbox.HelperCommand(t, "app", "trapp")
	// Ensure the child has a blessing that (a) allows it talk back to the
	// node manager config service and (b) allows the node manager to talk
	// to the app.  We achieve this by making the app's blessing derived
	// from the node manager's blessing post claiming (which will be
	// "mydevice").
	defer setupChildCommandWithBlessing(app, "mydevice/child")()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	nodeStub, err := node.BindNode("nm//nm")
	if err != nil {
		t.Fatalf("BindNode failed: %v", err)
	}

	selfRT := rt.R()
	otherRT := newRuntime(t)
	defer otherRT.Cleanup()

	// Nodemanager should have open ACLs before we claim it and so an Install from otherRT should succeed.
	if err = tryInstall(otherRT); err != nil {
		t.Fatal(err)
	}
	// Claim the nodemanager with selfRT as <defaultblessing>/mydevice
	if err = nodeStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "mydevice"}); err != nil {
		t.Fatal(err)
	}
	// Installation should succeed since rt.R() (a.k.a. selfRT) is now the
	// "owner" of the nodemanager.
	appID := installApp(t)

	// otherRT should be unable to install though, since the ACLs have changed now.
	if err = tryInstall(otherRT); err == nil {
		t.Fatalf("Install should have failed from otherRT")
	}

	// Create a script wrapping the test target that implements suidhelper.
	generateSuidHelperScript(t, root)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	// Start an instance of the app.
	instanceID := startApp(t, appID)
	<-pingCh
	resolve(t, "trapp", 1)
	suspendApp(t, appID, instanceID)

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

	var (
		proot = newRootPrincipal("root")
		// The two "processes"/runtimes which will act as IPC clients to the
		// nodemanager process.
		selfRT  = rt.R()
		otherRT = newRuntime(t)
	)
	defer otherRT.Cleanup()
	// By default, selfRT and otherRT will have blessings generated based on the
	// username/machine name running this process. Since these blessings will appear
	// in ACLs, give them recognizable names.
	if err := setDefaultBlessings(selfRT.Principal(), proot, "self"); err != nil {
		t.Fatal(err)
	}
	if err := setDefaultBlessings(otherRT.Principal(), proot, "other"); err != nil {
		t.Fatal(err)
	}

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
	acl, etag, err := nodeStub.GetACL(selfRT.NewContext())
	if err != nil {
		t.Fatalf("GetACL failed:%v", err)
	}
	if etag != "default" {
		t.Fatalf("getACL expected:default, got:%v(%v)", etag, acl)
	}

	// Claim the nodemanager as "root/self/mydevice"
	if err = nodeStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "mydevice"}); err != nil {
		t.Fatal(err)
	}
	expectedACL := security.ACL{In: map[security.BlessingPattern]security.LabelSet{"root/self/mydevice": security.AllLabels}}
	var b bytes.Buffer
	if err := vsecurity.SaveACL(&b, expectedACL); err != nil {
		t.Fatalf("Failed to saveACL:%v", err)
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
	if err = tryInstall(otherRT); err == nil {
		t.Fatalf("Install should have failed with random identity")
	}
	newACL := security.ACL{In: map[security.BlessingPattern]security.LabelSet{"root/other": security.AllLabels}}
	if err = nodeStub.SetACL(selfRT.NewContext(), newACL, "invalid"); err == nil {
		t.Fatalf("SetACL should have failed with invalid etag")
	}
	if err = nodeStub.SetACL(selfRT.NewContext(), newACL, etag); err != nil {
		t.Fatal(err)
	}
	// Install should now fail with selfRT, which no longer matches the ACLs but succeed with otherRT, which does.
	if err = tryInstall(selfRT); err == nil {
		t.Errorf("Install should have failed with selfRT since it should no longer match the ACL")
	}
	if err = tryInstall(otherRT); err != nil {
		t.Error(err)
	}
}

// install installs the node manager.
func install(args []string) {
	if err := impl.SelfInstall(args); err != nil {
		vlog.Fatalf("SelfInstall failed: %v", err)
	}
}

// TestNodeManagerInstall verifies the 'self install' functionality of the node
// manager: it runs SelfInstall in a child process, then runs the executable
// from the soft link that the installation created.  This should bring up a
// functioning node manager.
func TestNodeManagerInstall(t *testing.T) {
	defer setupLocalNamespace(t)()
	root, cleanup := setupRootDir(t)
	defer cleanup()

	// Current link does not have to live in the root dir, but it's
	// convenient to put it there so we have everything in one place.
	currLink := filepath.Join(root, "current_link")

	// We create this command not to run it, but to harvest the flags and
	// env vars from it.  We need these to pass to the installer, to ensure
	// that the node manager that the installer configures can run.
	nm := blackbox.HelperCommand(t, "nodeManager", "nm")
	defer setupChildCommand(nm)()

	argsForNodeManager := append([]string{"--"}, nm.Cmd.Args[1:]...)
	installer := blackbox.HelperCommand(t, "installer", argsForNodeManager...)
	installer.Cmd.Env = exec.Mergeenv(installer.Cmd.Env, nm.Cmd.Env)

	// Add vars to instruct the installer how to configure the node manager.
	installer.Cmd.Env = exec.Setenv(installer.Cmd.Env, config.RootEnv, root)
	installer.Cmd.Env = exec.Setenv(installer.Cmd.Env, config.CurrentLinkEnv, currLink)

	if err := installer.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	installer.ExpectEOFAndWait()
	installer.Cleanup()

	// CurrLink should now be pointing to a node manager script that
	// can start up a node manager.

	runNM := blackbox.HelperCommand(t, "execScript", currLink)
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	pid := readPID(t, runNM)
	resolve(t, "nm", 1)
	revertNodeExpectError(t, "nm", verror.NoExist) // No previous version available.
	syscall.Kill(pid, syscall.SIGINT)
	runNM.Expect("nm terminating")
	runNM.ExpectEOFAndWait()
	runNM.Cleanup()
}

func TestNodeManagerGlob(t *testing.T) {
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

	// Create a script wrapping the test target that implements suidhelper.
	generateSuidHelperScript(t, root)

	// Create the local server that the app uses to let us know it's ready.
	pingCh, cleanup := setupPingServer(t)
	defer cleanup()

	// Create the envelope for the first version of the app.
	app := blackbox.HelperCommand(t, "app", "appV1")
	defer setupChildCommand(app)()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	// Install the app.
	appID := installApp(t)
	installID := path.Base(appID)

	// Start an instance of the app.
	instance1ID := startApp(t, appID)
	<-pingCh // Wait until the app pings us that it's ready.

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
			"nm",
		}},
		{"nm/apps", "*", []string{"google naps"}},
		{"nm/apps/google naps", "*", []string{installID}},
	}
	for _, tc := range testcases {
		c, err := mounttable.BindGlobbable(tc.name)
		if err != nil {
			t.Fatalf("BindGlobbable failed: %v", err)
		}

		stream, err := c.Glob(rt.R().NewContext(), tc.pattern)
		if err != nil {
			t.Errorf("Glob failed: %v", err)
		}
		results := []string{}
		iterator := stream.RecvStream()
		for iterator.Advance() {
			results = append(results, iterator.Value().Name)
		}
		sort.Strings(results)
		if !reflect.DeepEqual(results, tc.expected) {
			t.Errorf("unexpected result. Got %q, want %q", results, tc.expected)
		}
		if err := iterator.Err(); err != nil {
			t.Errorf("unexpected stream error: %v", err)
		}
		if err := stream.Finish(); err != nil {
			t.Errorf("Finish failed: %v", err)
		}
	}
}

// rootPrincipal encapsulates a principal that acts as an "identity provider".
type rootPrincipal struct {
	p security.Principal
	b security.Blessings
}

func (r *rootPrincipal) Bless(key security.PublicKey, as string) (security.Blessings, error) {
	return r.p.Bless(key, r.b, as, security.UnconstrainedUse())
}

func newRootPrincipal(name string) *rootPrincipal {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		panic(err)
	}
	b, err := p.BlessSelf(name)
	if err != nil {
		panic(err)
	}
	return &rootPrincipal{p, b}
}

func setDefaultBlessings(p security.Principal, root *rootPrincipal, name string) error {
	b, err := root.Bless(p.PublicKey(), name)
	if err != nil {
		return err
	}
	tsecurity.SetDefaultBlessings(p, b)
	return nil
}

func listAndVerifyAssociations(t *testing.T, stub node.Node, run veyron2.Runtime, expected []node.Association) {
	assocs, err := stub.ListAssociations(run.NewContext())
	if err != nil {
		t.Fatalf("ListAssociations failed %v", err)
	}
	compareAssociations(t, assocs, expected)
}

// TODO(rjkroege): Verify that associations persist across restarts
// once permanent storage is added.
func TestAccountAssociation(t *testing.T) {
	defer setupLocalNamespace(t)()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	var (
		proot = newRootPrincipal("root")
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
	if err := setDefaultBlessings(selfRT.Principal(), proot, "self"); err != nil {
		t.Fatal(err)
	}
	if err := setDefaultBlessings(otherRT.Principal(), proot, "other"); err != nil {
		t.Fatal(err)
	}

	// Set up the node manager.
	nm := blackbox.HelperCommand(t, "nodeManager", "nm", root, "unused app repo name", "unused curr link")
	defer setupChildCommand(nm)()
	if err := nm.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer nm.Cleanup()
	readPID(t, nm)

	nodeStub, err := node.BindNode("nm//nm")
	if err != nil {
		t.Fatalf("BindNode failed %v", err)
	}

	// Attempt to list associations on the node manager without having
	// claimed it.
	if list, err := nodeStub.ListAssociations(otherRT.NewContext()); err != nil || list != nil {
		t.Fatalf("ListAssociations should fail on unclaimed node manager but did not: %v", err)
	}
	// self claims the node manager.
	if err = nodeStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "alice"}); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	vlog.VI(2).Info("Verify that associations start out empty.")
	listAndVerifyAssociations(t, nodeStub, selfRT, []node.Association(nil))

	if err = nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/self", "root/other"}, "alice_system_account"); err != nil {
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

	if err = nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/self", "root/other"}, "alice_other_account"); err != nil {
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

	err = nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, "")
	if err != nil {
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
	// Set up mount table, application, and binary repositories.
	defer setupLocalNamespace(t)()
	envelope, cleanup := startApplicationRepository()
	defer cleanup()
	defer startBinaryRepository()()

	root, cleanup := setupRootDir(t)
	defer cleanup()

	var (
		proot = newRootPrincipal("root")
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
	if err := setDefaultBlessings(selfRT.Principal(), proot, "self"); err != nil {
		t.Fatal(err)
	}
	if err := setDefaultBlessings(otherRT.Principal(), proot, "other"); err != nil {
		t.Fatal(err)
	}

	// Create a script wrapping the test target that implements
	// suidhelper.
	generateSuidHelperScript(t, root)

	// Set up the node manager.  Since we won't do node manager updates,
	// don't worry about its application envelope and current link.
	nm := blackbox.HelperCommand(t, "nodeManager", "-mocksetuid", "nm", root, "unused app repo name", "unused curr link")
	defer setupChildCommand(nm)()
	if err := nm.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer nm.Cleanup()
	readPID(t, nm)

	nodeStub, err := node.BindNode("nm//nm")
	if err != nil {
		t.Fatalf("BindNode failed %v", err)
	}

	// Create the local server that the app uses to tell us which system name
	// the node manager wished to run it as.
	server, _ := newServer()
	defer server.Stop()
	pingCh := make(chan string, 1)
	if err := server.Serve("pingserver", ipc.LeafDispatcher(pingServerDisp(pingCh), nil)); err != nil {
		t.Fatalf("Serve(%q, <dispatcher>) failed: %v", "pingserver", err)
	}

	// Create an envelope for the app.
	app := blackbox.HelperCommand(t, "app", "appV1")
	defer setupChildCommandWithBlessing(app, "alice/child")()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	// Install and start the app as root/self.
	appID := installApp(t, selfRT)

	// Claim the nodemanager with selfRT as root/self/alice
	if err = nodeStub.Claim(selfRT.NewContext(), &granter{p: selfRT.Principal(), extension: "alice"}); err != nil {
		t.Fatal(err)
	}

	// Start an instance of the app but this time it should fail: we do
	// not have an associated uname for the invoking identity.
	startAppExpectError(t, appID, verror.NoAccess, selfRT)

	// Create an association for selfRT
	if err = nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/self"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	instance1ID := startApp(t, appID, selfRT)
	verifyHelperArgs(t, <-pingCh, testUserName) // Wait until the app pings us that it's ready.
	stopApp(t, appID, instance1ID, selfRT)

	vlog.VI(2).Infof("other attempting to run an app without access. Should fail.")
	startAppExpectError(t, appID, verror.NoAccess, otherRT)

	// Self will now let other also run apps.
	if err = nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, testUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}
	// Add Start to the ACL list for root/other.
	newACL, _, err := nodeStub.GetACL(selfRT.NewContext())
	if err != nil {
		t.Fatalf("GetACL failed %v", err)
	}
	newACL.In["root/other/..."] = security.AllLabels
	if err = nodeStub.SetACL(selfRT.NewContext(), newACL, ""); err != nil {
		t.Fatalf("SetACL failed %v", err)
	}

	vlog.VI(2).Infof("other attempting to run an app with access. Should succeed.")
	instance2ID := startApp(t, appID, otherRT)
	verifyHelperArgs(t, <-pingCh, testUserName) // Wait until the app pings us that it's ready.
	suspendApp(t, appID, instance2ID, otherRT)

	vlog.VI(2).Infof("Verify that Resume with the same systemName works.")
	resumeApp(t, appID, instance2ID, otherRT)
	verifyHelperArgs(t, <-pingCh, testUserName) // Wait until the app pings us that it's ready.
	suspendApp(t, appID, instance2ID, otherRT)

	// Change the associated system name.
	if err = nodeStub.AssociateAccount(selfRT.NewContext(), []string{"root/other"}, anotherTestUserName); err != nil {
		t.Fatalf("AssociateAccount failed %v", err)
	}

	vlog.VI(2).Infof("Show that Resume with a different systemName fails.")
	resumeAppExpectError(t, appID, instance2ID, verror.NoAccess, otherRT)

	// Clean up.
	stopApp(t, appID, instance2ID, otherRT)

	vlog.VI(2).Infof("Show that Start with different systemName works.")
	instance3ID := startApp(t, appID, otherRT)
	verifyHelperArgs(t, <-pingCh, anotherTestUserName) // Wait until the app pings us that it's ready.

	// Clean up.
	stopApp(t, appID, instance3ID, otherRT)
}

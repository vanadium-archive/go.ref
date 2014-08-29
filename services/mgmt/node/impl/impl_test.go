package impl_test

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	goexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"veyron/lib/signals"
	"veyron/lib/testutil/blackbox"
	tsecurity "veyron/lib/testutil/security"
	"veyron/services/mgmt/lib/exec"
	"veyron/services/mgmt/node/config"
	"veyron/services/mgmt/node/impl"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/security"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/node"
	"veyron2/verror"
	"veyron2/vlog"
)

// TestHelperProcess is blackbox boilerplate.
func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func init() {
	// All the tests and the subprocesses they start require a runtime; so just
	// create it here.
	rt.Init()

	// Disable the cache because we will be manipulating/using the namespace
	// across multiple processes and want predictable behaviour without
	// relying on timeouts.
	rt.R().Namespace().CacheCtl(naming.DisableCache(true))

	blackbox.CommandTable["execScript"] = execScript
	blackbox.CommandTable["nodeManager"] = nodeManager
	blackbox.CommandTable["app"] = app
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
	if call, err := rt.R().Client().StartCall(rt.R().NewContext(), "pingserver", "Ping", nil); err != nil {
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
	ping()
}

// generateScript is very similar in behavior to its namesake in invoker.go.
// However, we chose to re-implement it here for two reasons: (1) avoid making
// generateScript public; and (2) how the test choses to invoke the node manager
// subprocess the first time should be independent of how node manager
// implementation sets up its updated versions.
func generateScript(t *testing.T, root string, cmd *goexec.Cmd) string {
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
func setupRootDir() (string, func()) {
	// On some operating systems (e.g. darwin) os.TempDir() can return a
	// symlink. To avoid having to account for this eventuality later,
	// evaluate the symlink.
	tmpDir, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		vlog.Fatalf("EvalSymlinks(%v) failed: %v", os.TempDir(), err)
	}
	root := filepath.Join(tmpDir, "nodemanager")
	os.RemoveAll(root) // Start out with a clean slate.
	return root, func() {
		os.RemoveAll(root)
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

	root, cleanup := setupRootDir()
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
	scriptPathFactory := generateScript(t, root, nm.Cmd)

	if err := os.Symlink(scriptPathFactory, currLink); err != nil {
		t.Fatalf("Symlink(%q, %q) failed: %v", scriptPathFactory, currLink, err)
	}
	// We instruct the initial node manager that we run to pause before
	// stopping its service, so that we get a chance to verify that
	// attempting an update while another one is ongoing will fail.
	nm.Cmd.Env = exec.Setenv(nm.Cmd.Env, "PAUSE_BEFORE_STOP", "1")

	resolveExpectError(t, "factoryNM", verror.NotFound) // Ensure a clean slate.

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
	resolve(t, "factoryNM") // Verify the node manager has published itself.

	// Simulate an invalid envelope in the application repository.
	*envelope = *nodeEnvelopeFromCmd(nm.Cmd)
	envelope.Title = "bogus"
	updateExpectError(t, "factoryNM", verror.BadArg)   // Incorrect title.
	revertExpectError(t, "factoryNM", verror.NotFound) // No previous version available.

	// Set up a second version of the node manager.  We use the blackbox
	// command solely to collect the args and env we need to provide the
	// application repository with an envelope that will actually run the
	// node manager subcommand.  The blackbox command is never started by
	// hand -- instead, the information in the envelope will be used by the
	// node manager to stage the next version.
	nmV2 := blackbox.HelperCommand(t, "nodeManager", "v2NM")
	defer setupChildCommand(nmV2)()
	*envelope = *nodeEnvelopeFromCmd(nmV2.Cmd)
	update(t, "factoryNM")

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

	updateExpectError(t, "factoryNM", verror.Exists) // Update already in progress.

	nm.CloseStdin()
	nm.Expect("factoryNM terminating")
	deferrer = nil
	nm.Cleanup()

	// A successful update means the node manager has stopped itself.  We
	// relaunch it from the current link.
	runNM := blackbox.HelperCommand(t, "execScript", currLink)
	resolveExpectError(t, "v2NM", verror.NotFound) // Ensure a clean slate.
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer = runNM.Cleanup
	readPID(t, runNM)
	resolve(t, "v2NM") // Current link should have been launching v2.

	// Try issuing an update without changing the envelope in the application
	// repository: this should fail, and current link should be unchanged.
	updateExpectError(t, "v2NM", verror.NotFound)
	if evalLink() != scriptPathV2 {
		t.Fatalf("script changed")
	}

	// Create a third version of the node manager and issue an update.
	nmV3 := blackbox.HelperCommand(t, "nodeManager", "v3NM")
	defer setupChildCommand(nmV3)()
	*envelope = *nodeEnvelopeFromCmd(nmV3.Cmd)
	update(t, "v2NM")

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
	resolveExpectError(t, "v3NM", verror.NotFound) // Ensure a clean slate.
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer = runNM.Cleanup
	readPID(t, runNM)
	resolve(t, "v3NM") // Current link should have been launching v3.

	// Revert the node manager to its previous version (v2).
	revert(t, "v3NM")
	revertExpectError(t, "v3NM", verror.Exists) // Revert already in progress.
	runNM.CloseStdin()
	runNM.Expect("v3NM terminating")
	if evalLink() != scriptPathV2 {
		t.Fatalf("current link was not reverted correctly")
	}
	deferrer = nil
	runNM.Cleanup()

	// Re-launch the node manager from current link.
	runNM = blackbox.HelperCommand(t, "execScript", currLink)
	resolveExpectError(t, "v2NM", verror.NotFound) // Ensure a clean slate.
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer = runNM.Cleanup
	readPID(t, runNM)
	resolve(t, "v2NM") // Current link should have been launching v2.

	// Revert the node manager to its previous version (factory).
	revert(t, "v2NM")
	runNM.Expect("v2NM terminating")
	if evalLink() != scriptPathFactory {
		t.Fatalf("current link was not reverted correctly")
	}
	deferrer = nil
	runNM.Cleanup()

	// Re-launch the node manager from current link.
	runNM = blackbox.HelperCommand(t, "execScript", currLink)
	resolveExpectError(t, "factoryNM", verror.NotFound) // Ensure a clean slate.
	if err := runNM.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	deferrer = runNM.Cleanup
	pid := readPID(t, runNM)
	resolve(t, "factoryNM") // Current link should have been launching factory version.
	syscall.Kill(pid, syscall.SIGINT)
	runNM.Expect("factoryNM terminating")
	runNM.ExpectEOFAndWait()
}

type pingServerDisp chan<- struct{}

func (p pingServerDisp) Ping(ipc.ServerCall) { p <- struct{}{} }

func installApp(t *testing.T) string {
	appsName := "nm//apps"
	stub, err := node.BindApplication(appsName)
	if err != nil {
		t.Fatalf("BindApplication(%v) failed: %v", appsName, err)
	}
	appID, err := stub.Install(rt.R().NewContext(), "ar")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	return appID
}

func startAppImpl(t *testing.T, appID string) (string, error) {
	appsName := "nm//apps"
	appName := naming.Join(appsName, appID)
	stub, err := node.BindApplication(appName)
	if err != nil {
		t.Fatalf("BindApplication(%v) failed: %v", appName, err)
	}
	if instanceIDs, err := stub.Start(rt.R().NewContext()); err != nil {
		return "", err
	} else {
		if want, got := 1, len(instanceIDs); want != got {
			t.Fatalf("Expected %v instance ids, got %v instead", want, got)
		}
		return instanceIDs[0], nil
	}
}

func startApp(t *testing.T, appID string) string {
	instanceID, err := startAppImpl(t, appID)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	return instanceID
}

func startAppShouldFail(t *testing.T, appID string, expectedError verror.ID) {
	if _, err := startAppImpl(t, appID); err == nil || !verror.Is(err, expectedError) {
		t.Fatalf("Start(%v) expected to fail with %v, got %v instead", appID, expectedError, err)
	}
}

func stopApp(t *testing.T, appID, instanceID string) {
	appsName := "nm//apps"
	appName := naming.Join(appsName, appID)
	instanceName := naming.Join(appName, instanceID)
	stub, err := node.BindApplication(instanceName)
	if err != nil {
		t.Fatalf("BindApplication(%v) failed: %v", instanceName, err)
	}
	if err := stub.Stop(rt.R().NewContext(), 5); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func suspendApp(t *testing.T, appID, instanceID string) {
	appsName := "nm//apps"
	appName := naming.Join(appsName, appID)
	instanceName := naming.Join(appName, instanceID)
	stub, err := node.BindApplication(instanceName)
	if err != nil {
		t.Fatalf("BindApplication(%v) failed: %v", instanceName, err)
	}
	if err := stub.Suspend(rt.R().NewContext()); err != nil {
		t.Fatalf("Suspend failed: %v", err)
	}
}

func resumeApp(t *testing.T, appID, instanceID string) {
	appsName := "nm//apps"
	appName := naming.Join(appsName, appID)
	instanceName := naming.Join(appName, instanceID)
	stub, err := node.BindApplication(instanceName)
	if err != nil {
		t.Fatalf("BindApplication(%v) failed: %v", instanceName, err)
	}
	if err := stub.Resume(rt.R().NewContext()); err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
}

func uninstallApp(t *testing.T, appID string) {
	appsName := "nm//apps"
	appName := naming.Join(appsName, appID)
	stub, err := node.BindApplication(appName)
	if err != nil {
		t.Fatalf("BindApplication(%v) failed: %v", appName, err)
	}
	if err := stub.Uninstall(rt.R().NewContext()); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
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

// TestAppLifeCycle installs an app, starts it, suspends it, resumes it, and
// then stops it.
func TestAppLifeCycle(t *testing.T) {
	// Set up mount table, application, and binary repositories.
	defer setupLocalNamespace(t)()
	envelope, cleanup := startApplicationRepository()
	defer cleanup()
	defer startBinaryRepository()()

	root, cleanup := setupRootDir()
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

	// Create the local server that the app uses to let us know it's ready.
	server, _ := newServer()
	defer server.Stop()
	pingCh := make(chan struct{}, 1)
	if err := server.Serve("pingserver", ipc.LeafDispatcher(pingServerDisp(pingCh), nil)); err != nil {
		t.Fatalf("Serve(%q, <dispatcher>) failed: %v", "pingserver", err)
	}

	// Create an envelope for an app.
	app := blackbox.HelperCommand(t, "app", "app1")
	defer setupChildCommand(app)()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	// Install the app.
	appID := installApp(t)

	// Start the app.
	instanceID := startApp(t, appID)
	<-pingCh // Wait until the app pings us that it's ready.

	// Suspend the app.
	suspendApp(t, appID, instanceID)
	<-pingCh // App should have pinged us before it terminated.

	resumeApp(t, appID, instanceID)
	<-pingCh

	// TODO(caprita): test Suspend and Resume, and verify various
	// non-standard combinations (suspend when stopped; resume while still
	// running; stop while suspended).

	// Stop the app.
	stopApp(t, appID, instanceID)
	<-pingCh // App should have pinged us before it terminated.

	verifyAppWorkspace(t, root, appID, instanceID)

	// Uninstall the app.
	uninstallApp(t, appID)

	// Starting new instances should no longer be allowed.
	startAppShouldFail(t, appID, verror.BadArg)

	// Cleanly shut down the node manager.
	syscall.Kill(nm.Cmd.Process.Pid, syscall.SIGINT)
	nm.Expect("nm terminating")
	nm.ExpectEOFAndWait()
}

type granter struct {
	ipc.CallOpt
	self security.PrivateID
}

func (g granter) Grant(id security.PublicID) (security.PublicID, error) {
	return g.self.Bless(id, "claimernode", 10*time.Minute, nil)
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

	root, cleanup := setupRootDir()
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
	app := blackbox.HelperCommand(t, "app", "app1")
	defer setupChildCommand(app)()
	appTitle := "google naps"
	*envelope = *envelopeFromCmd(appTitle, app.Cmd)

	nodeStub, err := node.BindNode("nm//nm")
	if err != nil {
		t.Fatalf("BindNode failed: %v", err)
	}

	// Create a new identity and runtime.
	newIdentity := tsecurity.NewBlessedIdentity(rt.R().Identity(), "claimer")
	newRT, nodeClient := newRuntimeClient(t, newIdentity)
	defer newRT.Cleanup()

	// Nodemanager should have open ACLs before we claim it and so an Install
	// should succeed.
	if err = tryInstall(newRT, nodeClient); err != nil {
		t.Fatalf("%v", err)
	}
	// Claim the nodemanager with this identity.
	if err = nodeStub.Claim(rt.R().NewContext(), granter{self: newIdentity}); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if err = tryInstall(newRT, nodeClient); err != nil {
		t.Fatalf("%v", err)
	}
	// Try to install with a new identity. This should fail.
	newIdentity = tsecurity.NewBlessedIdentity(rt.R().Identity(), "random")
	newRT, nodeClient = newRuntimeClient(t, newIdentity)
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

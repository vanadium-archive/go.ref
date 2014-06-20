package impl_test

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"veyron/lib/exec"
	"veyron/lib/signals"
	"veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	"veyron/services/mgmt/node"
	"veyron/services/mgmt/node/impl"
	mtlib "veyron/services/mounttable/lib"

	"veyron2"
	"veyron2/ipc"
	"veyron2/mgmt"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/content"
	"veyron2/vlog"
)

const (
	testEnv = "VEYRON_NM_TEST"
)

var (
	errOperationFailed = errors.New("operation failed")
)

func init() {
	blackbox.CommandTable["nodeManager"] = nodeManager
}

// arInvoker holds the state of an application repository invocation
// mock.
type arInvoker struct{}

// APPLICATION REPOSITORY INTERFACE IMPLEMENTATION

func (i *arInvoker) Match(ipc.ServerContext, []string) (application.Envelope, error) {
	vlog.VI(0).Infof("Match()")
	envelope := generateEnvelope()
	envelope.Env = exec.Setenv(envelope.Env, testEnv, "child")
	envelope.Binary = "cr"
	return *envelope, nil
}

// crInvoker holds the state of a content repository invocation mock.
type crInvoker struct{}

// CONTENT REPOSITORY INTERFACE IMPLEMENTATION

func (i *crInvoker) Delete(ipc.ServerContext) error {
	vlog.VI(0).Infof("Delete()")
	return nil
}

func (i *crInvoker) Download(_ ipc.ServerContext, stream content.RepositoryServiceDownloadStream) error {
	vlog.VI(0).Infof("Download()")
	file, err := os.Open(os.Args[0])
	if err != nil {
		vlog.Errorf("Open() failed: %v", err)
		return errOperationFailed
	}
	defer file.Close()
	bufferLength := 4096
	buffer := make([]byte, bufferLength)
	for {
		n, err := file.Read(buffer)
		switch err {
		case io.EOF:
			return nil
		case nil:
			if err := stream.Send(buffer[:n]); err != nil {
				vlog.Errorf("Send() failed: %v", err)
				return errOperationFailed
			}
		default:
			vlog.Errorf("Read() failed: %v", err)
			return errOperationFailed
		}
	}
}

func (i *crInvoker) Upload(ipc.ServerContext, content.RepositoryServiceUploadStream) (string, error) {
	vlog.VI(0).Infof("Upload()")
	return "", nil
}

func generateBinary(workspace string) string {
	path := filepath.Join(workspace, "noded")
	if err := os.Link(os.Args[0], path); err != nil {
		vlog.Fatalf("Link(%v, %v) failed: %v", os.Args[0], path, err)
	}
	return path
}

func generateEnvelope() *application.Envelope {
	envelope := &application.Envelope{}
	envelope.Args = os.Args[1:]
	re := regexp.MustCompile("^([^=]*)=(.*)$")
	for _, env := range os.Environ() {
		envelope.Env = append(envelope.Env, re.ReplaceAllString(env, "$1=\"$2\""))
	}
	return envelope
}

func generateLink(root, workspace string) {
	link := filepath.Join(root, impl.CurrentWorkspace)
	newLink := link + ".new"
	fi, err := os.Lstat(newLink)
	if err == nil {
		if err := os.Remove(fi.Name()); err != nil {
			vlog.Fatalf("Remove(%v) failed: %v", fi.Name(), err)
		}
	}
	if err := os.Symlink(workspace, newLink); err != nil {
		vlog.Fatalf("Symlink(%v, %v) failed: %v", workspace, newLink, err)
	}
	if err := os.Rename(newLink, link); err != nil {
		vlog.Fatalf("Rename(%v, %v) failed: %v", newLink, link, err)
	}
}

func generateScript(workspace, binary string) string {
	envelope := generateEnvelope()
	envelope.Env = exec.Setenv(envelope.Env, testEnv, "parent")
	output := "#!/bin/bash\n"
	output += strings.Join(envelope.Env, " ") + " "
	output += binary + " " + strings.Join(envelope.Args, " ")
	path := filepath.Join(workspace, "noded.sh")
	if err := ioutil.WriteFile(path, []byte(output), 0755); err != nil {
		vlog.Fatalf("WriteFile(%v) failed: %v", path, err)
	}
	return path
}

func invokeCallback(name string) {
	handle, err := exec.GetChildHandle()
	switch err {
	case nil:
		// Node manager was started by self-update, notify the parent
		// process that you are ready.
		handle.SetReady()
		callbackName, err := handle.Config.Get(mgmt.ParentNodeManagerConfigKey)
		if err != nil {
			vlog.Fatalf("Failed to get callback name from config: %v", err)
		}
		nmClient, err := node.BindNode(callbackName)
		if err != nil {
			vlog.Fatalf("BindNode(%v) failed: %v", callbackName, err)
		}
		if err := nmClient.Set(rt.R().NewContext(), mgmt.ChildNodeManagerConfigKey, name); err != nil {
			vlog.Fatalf("Set(%v, %v) failed: %v", mgmt.ChildNodeManagerConfigKey, name, err)
		}
	case exec.ErrNoVersion:
		vlog.Fatalf("invokeCallback should only be called from child node manager")
	default:
		vlog.Fatalf("NewChildHandle() failed: %v", err)
	}
}

func invokeUpdate(t *testing.T, name string) {
	address := naming.JoinAddressName(name, "nm")
	stub, err := node.BindNode(address)
	if err != nil {
		t.Fatalf("BindNode(%v) failed: %v", address, err)
	}
	if err := stub.Update(rt.R().NewContext()); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}
}

// nodeManager is an enclosure for setting up and starting the parent
// and child node manager used by the TestUpdate() method.
func nodeManager(argv []string) {
	root := os.Getenv(impl.RootEnv)
	switch os.Getenv(testEnv) {
	case "setup":
		workspace := filepath.Join(root, fmt.Sprintf("%v", time.Now().Format(time.RFC3339Nano)))
		perm := os.FileMode(0755)
		if err := os.MkdirAll(workspace, perm); err != nil {
			vlog.Fatalf("MkdirAll(%v, %v) failed: %v", workspace, perm, err)
		}
		binary := generateBinary(workspace)
		script := generateScript(workspace, binary)
		generateLink(root, workspace)
		argv, envv := []string{}, []string{}
		if err := syscall.Exec(script, argv, envv); err != nil {
			vlog.Fatalf("Exec(%v, %v, %v) failed: %v", script, argv, envv, err)
		}
	case "parent":
		runtime := rt.Init()
		defer runtime.Shutdown()
		// Set up a mock content repository, a mock application repository, and a node manager.
		_, crCleanup := startContentRepository()
		defer crCleanup()
		_, arCleanup := startApplicationRepository()
		defer arCleanup()
		_, nmCleanup := startNodeManager()
		defer nmCleanup()
		// Wait until shutdown.
		<-signals.ShutdownOnSignals()
		blackbox.WaitForEOFOnStdin()
	case "child":
		runtime := rt.Init()
		defer runtime.Shutdown()
		// Set up a node manager.
		name, nmCleanup := startNodeManager()
		defer nmCleanup()
		invokeCallback(name)
		// Wait until shutdown.
		<-signals.ShutdownOnSignals()
		blackbox.WaitForEOFOnStdin()
	}
}

func spawnNodeManager(t *testing.T, mtName string, idFile string) *blackbox.Child {
	root := filepath.Join(os.TempDir(), "noded")
	child := blackbox.HelperCommand(t, "nodeManager")
	child.Cmd.Env = exec.Setenv(child.Cmd.Env, "NAMESPACE_ROOT", mtName)
	child.Cmd.Env = exec.Setenv(child.Cmd.Env, "VEYRON_IDENTITY", idFile)
	child.Cmd.Env = exec.Setenv(child.Cmd.Env, impl.OriginEnv, "ar")
	child.Cmd.Env = exec.Setenv(child.Cmd.Env, impl.RootEnv, root)
	child.Cmd.Env = exec.Setenv(child.Cmd.Env, testEnv, "setup")
	if err := child.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	return child
}

func startApplicationRepository() (string, func()) {
	server, err := rt.R().NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	suffix, dispatcher := "", ipc.SoloDispatcher(application.NewServerRepository(&arInvoker{}), nil)
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	vlog.VI(1).Infof("Application repository running at endpoint: %s", endpoint)
	name := "ar"
	if err := server.Publish(name); err != nil {
		vlog.Fatalf("Publish(%v) failed: %v", name, err)
	}
	return name, func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

func startContentRepository() (string, func()) {
	server, err := rt.R().NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	suffix, dispatcher := "", ipc.SoloDispatcher(content.NewServerRepository(&crInvoker{}), nil)
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	vlog.VI(1).Infof("Content repository running at endpoint: %s", endpoint)
	name := "cr"
	if err := server.Publish(name); err != nil {
		vlog.Fatalf("Publish(%v) failed: %v", name, err)
	}
	return name, func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

func startMountTable(t *testing.T) (string, func()) {
	server, err := rt.R().NewServer(veyron2.ServesMountTableOpt(true))
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	dispatcher, err := mtlib.NewMountTable("")
	if err != nil {
		t.Fatalf("NewMountTable() failed: %v", err)
	}
	suffix := "mt"
	if err := server.Register(suffix, dispatcher); err != nil {
		t.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	name := naming.JoinAddressName(endpoint.String(), suffix)
	vlog.VI(1).Infof("Mount table name: %v", name)
	return name, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func startNodeManager() (string, func()) {
	server, err := rt.R().NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	suffix, envelope := "", &application.Envelope{}
	name := naming.MakeTerminal(naming.JoinAddressName(endpoint.String(), suffix))
	vlog.VI(0).Infof("Node manager name: %v", name)
	// TODO(jsimsa): Replace <PreviousEnv> with a command-line flag when
	// command-line flags in tests are supported.
	dispatcher := impl.NewDispatcher(nil, envelope, name, os.Getenv(impl.PreviousEnv))
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	publishAs := "nm"
	if err := server.Publish(publishAs); err != nil {
		vlog.Fatalf("Publish(%v) failed: %v", publishAs, err)
	}
	fmt.Printf("ready\n")
	return name, func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

// TestUpdate checks that the node manager Update() method works as
// expected. To that end, this test spawns a new process that invokes
// the nodeManager() method. The behavior of this method depends on
// the value of the VEYRON_NM_TEST environment variable:
//
// 1) Initially, the value of VEYRON_NM_TEST is set to be "setup",
// which prompts the nodeManager() method to setup a new workspace
// that mimics the structure used for storing the node manager
// binary. The method then sets VEYRON_NM_TEST to "parent" and
// restarts itself using syscall.Exec(), effectively becoming the
// process described next.
//
// 2) The "parent" branch sets up a mock application and content
// repository and a node manager that is pointed to the mock
// application repository for updates. When all three services start,
// the TestUpdate() method is notified and it proceeds to invoke
// Update() on the node manager. This in turn results in the node
// manager downloading an application envelope from the mock
// application repository and a binary from the mock content
// repository. These are identical to the application envelope of the
// "parent" node manager, except for the VEYRON_NM_TEST variable,
// which is set to "child". The Update() method then spawns the child
// node manager, checks that it is a valid node manager, and
// terminates.
//
// 3) The "child" branch sets up a node manager and then calls back to
// the "parent" node manager. This prompts the parent node manager to
// invoke the Revert() method on the child node manager, which
// terminates the child.
func TestUpdate(t *testing.T) {
	// Set up a mount table.
	runtime := rt.Init()
	mtName, mtCleanup := startMountTable(t)
	defer mtCleanup()
	ns := runtime.Namespace()
	// The local, client-side Namespace is now relative to the
	// MountTable server started above.
	ns.SetRoots([]string{mtName})
	// Spawn a node manager with an identity blessed by the MountTable's
	// identity under the name "test", and obtain its address.
	//
	// TODO(ataly): Eventually we want to use the same identity the node
	// manager would have if it was running in production.
	idFile := testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(runtime.Identity(), "test"))
	defer os.Remove(idFile)
	child := spawnNodeManager(t, mtName, idFile)
	defer child.Cleanup()
	child.Expect("ready")
	ctx, name := runtime.NewContext(), naming.Join(mtName, "nm")
	results, err := ns.Resolve(ctx, name)
	if err != nil {
		t.Fatalf("Resolve(%v) failed: %v", name, err)
	}
	if expected, got := 1, len(results); expected != got {
		t.Fatalf("Unexpected number of results: expected %d, got %d", expected, got)
	}
	invokeUpdate(t, name)
	child.Expect("ready")
}

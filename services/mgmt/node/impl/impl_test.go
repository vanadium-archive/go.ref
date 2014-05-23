package impl_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"

	"veyron/lib/signals"
	"veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	"veyron/runtimes/google/lib/exec"
	"veyron/services/mgmt/node/impl"
	mtlib "veyron/services/mounttable/lib"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/content"
	"veyron2/services/mgmt/node"
	"veyron2/vlog"
)

var (
	errOperationFailed = errors.New("operation failed")
)

type arInvoker struct {
	envelope *application.Envelope
}

func (i *arInvoker) Match(_ ipc.Context, _ []string) (application.Envelope, error) {
	vlog.VI(1).Infof("Match()")
	return *i.envelope, nil
}

const bufferLength = 1024

type cmInvoker struct{}

func (i *cmInvoker) Delete(_ ipc.Context) error {
	return nil
}

func (i *cmInvoker) Download(_ ipc.Context, stream content.ContentServiceDownloadStream) error {
	vlog.VI(1).Infof("Download()")
	file, err := os.Open(os.Args[0])
	if err != nil {
		vlog.Errorf("Open() failed: %v", err)
		return errOperationFailed
	}
	defer file.Close()
	buffer := make([]byte, bufferLength)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			vlog.Errorf("Read() failed: %v", err)
			return errOperationFailed
		}
		if n == 0 {
			break
		}
		if err := stream.Send(buffer[:n]); err != nil {
			vlog.Errorf("Send() failed: %v", err)
			return errOperationFailed
		}
	}
	return nil
}

func (i *cmInvoker) Upload(_ ipc.Context, _ content.ContentServiceUploadStream) (string, error) {
	return "", nil
}

func init() {
	blackbox.CommandTable["nodeManager"] = nodeManager
}

func getProcessID(t *testing.T, child *blackbox.Child) int {
	line, err := child.ReadLineFromChild()
	if err != nil {
		child.Cleanup()
		t.Fatalf("ReadLineFromChild() failed: %v", err)
	}
	pid, err := strconv.Atoi(line)
	if err != nil {
		t.Fatalf("Atoi(%v) failed: %v", line, err)
	}
	return pid
}

func invokeUpdate(t *testing.T, nmAddress string) {
	address := naming.JoinAddressName(nmAddress, "nm")
	nmClient, err := node.BindNode(address)
	if err != nil {
		t.Fatalf("BindNode(%v) failed: %v", address, err)
	}
	if err := nmClient.Update(); err != nil {
		t.Fatalf("%v.Update() failed: %v", address, err)
	}
}

// nodeManager is an enclosure for the node manager blackbox process.
func nodeManager(argv []string) {
	origin := argv[0]
	runtime := rt.Init()
	defer runtime.Shutdown()

	_, nmCleanup := startNodeManager(runtime, origin)
	defer nmCleanup()
	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
	blackbox.WaitForEOFOnStdin()
}

func spawnNodeManager(t *testing.T, arAddress, mtAddress string, idFile string) *blackbox.Child {
	child := blackbox.HelperCommand(t, "nodeManager", arAddress)
	child.Cmd.Env = append(child.Cmd.Env, fmt.Sprintf("MOUNTTABLE_ROOT=%v", mtAddress), fmt.Sprintf("VEYRON_IDENTITY=%v", idFile))
	if err := child.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	return child
}

func startApplicationRepository(t *testing.T, runtime veyron2.Runtime, cmAddress string, envelope *application.Envelope) (string, func()) {
	server, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	suffix, dispatcher := "ar", ipc.SoloDispatcher(application.NewServerRepository(&arInvoker{envelope: envelope}), nil)
	if err := server.Register(suffix, dispatcher); err != nil {
		t.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	address := naming.JoinAddressName(endpoint.String(), suffix)
	vlog.VI(1).Infof("Application repository running at endpoint: %s", address)
	return address, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func startContentManager(t *testing.T, runtime veyron2.Runtime) (string, func()) {
	server, err := runtime.NewServer()
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	suffix, dispatcher := "cm", ipc.SoloDispatcher(content.NewServerContent(&cmInvoker{}), nil)
	if err := server.Register(suffix, dispatcher); err != nil {
		t.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		t.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	address := naming.JoinAddressName(endpoint.String(), suffix)
	vlog.VI(1).Infof("Content manager running at endpoint: %s", address)
	return address, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func startMountTable(t *testing.T, runtime veyron2.Runtime) (string, func()) {
	server, err := runtime.NewServer()
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
	address := naming.JoinAddressName(endpoint.String(), suffix)
	vlog.VI(1).Infof("Mount table running at endpoint: %s", address)
	return address, func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("Stop() failed: %v", err)
		}
	}
}

func startNodeManager(runtime veyron2.Runtime, origin string) (string, func()) {
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	suffix, dispatcher := "", impl.NewDispatcher(&application.Envelope{}, origin)
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	address := naming.JoinAddressName(endpoint.String(), suffix)
	vlog.VI(1).Infof("Node manager running at endpoint: %q", address)
	name := "nm"
	if err := server.Publish(name); err != nil {
		vlog.Fatalf("Publish(%v) failed: %v", name, err)
	}
	handle, err := exec.NewChildHandle()
	switch err {
	case nil:
		handle.SetReady()
	case exec.ErrNoVersion:
	default:
		vlog.Fatalf("NewChildHandle() failed: %v", err)
	}
	fmt.Printf("%d\n", os.Getpid())
	return address, func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func TestUpdate(t *testing.T) {
	// Set up a mount table, a content manager, and an application repository.
	runtime := rt.Init()
	defer runtime.Shutdown()
	mtAddress, mtCleanup := startMountTable(t, runtime)
	defer mtCleanup()
	mt := runtime.MountTable()
	cmAddress, cmCleanup := startContentManager(t, runtime)
	defer cmCleanup()
	envelope := application.Envelope{}
	arAddress, arCleanup := startApplicationRepository(t, runtime, cmAddress, &envelope)
	defer arCleanup()

	// Spawn a node manager with an identity blessed by the mounttable's identity.
	// under the name "test", and obtain its endpoint.
	// TODO(ataly): Eventually we want to use the same identity the node manager
	// would have if it was running in production.

	idFile := testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(runtime.Identity(), "test"))
	defer os.Remove(idFile)
	child := spawnNodeManager(t, arAddress, mtAddress, idFile)
	defer child.Cleanup()
	_ = getProcessID(t, child) // sync with the child
	envelope.Args = child.Cmd.Args[1:]
	envelope.Env = child.Cmd.Env
	envelope.Binary = cmAddress

	name := naming.Join(mtAddress, "nm")
	results, err := mt.Resolve(name)
	if err != nil {
		t.Fatalf("Resolve(%v) failed: %v", name, err)
	}
	if expected, got := 1, len(results); expected != got {
		t.Fatalf("Unexpected number of results: expected %d, got %d", expected, got)
	}
	nmAddress := results[0]
	vlog.VI(1).Infof("Node manager running at endpoint: %q -> %s", name, nmAddress)

	// Invoke the Update method and check that another instance of the
	// node manager binary has been started.
	invokeUpdate(t, nmAddress)
	pid := getProcessID(t, child)

	if results, err := mt.Resolve(name); err != nil {
		t.Fatalf("Resolve(%v) failed: %v", name, err)
	} else {
		if expected, got := 2, len(results); expected != got {
			t.Fatalf("Unexpected number of results: expected %d, got %d", expected, got)
		}
	}

	// Terminate the node manager binary.
	//
	// TODO(jsimsa): When support for remote Stop() is implemented, use
	// it here instead.
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("FindProcess(%v) failed: %v", pid, err)
	}
	if err := process.Kill(); err != nil {
		t.Fatalf("Kill() failed: %v", err)
	}
}

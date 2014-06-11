package impl

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"veyron/lib/exec"
	"veyron/lib/signals"
	"veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"
	"veyron/services/mgmt/node"
	mtlib "veyron/services/mounttable/lib"

	"veyron2"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/services/mgmt/application"
	"veyron2/vlog"
)

func init() {
	blackbox.CommandTable["nodeManager"] = nodeManager
}

func invokeCallback(name string) {
	handle, err := exec.GetChildHandle()
	switch err {
	case nil:
		// Node manager was started by self-update, notify the parent
		// process that you are ready.
		handle.SetReady()
		if handle.CallbackName != "" {
			nmClient, err := node.BindNode(handle.CallbackName)
			if err != nil {
				vlog.Fatalf("BindNode(%v) failed: %v", handle.CallbackName, err)
			}
			if err := nmClient.Callback(rt.R().NewContext(), name); err != nil {
				vlog.Fatalf("Callback(%v) failed: %v", name, err)
			}
		}
	case exec.ErrNoVersion:
		// Node manager was not started by self-update, no action is
		// needed.
	default:
		vlog.Fatalf("NewChildHandle() failed: %v", err)
	}
}

func invokeUpdate(t *testing.T, nmAddress string) {
	address := naming.JoinAddressName(nmAddress, "nm")
	nmClient, err := node.BindNode(address)
	if err != nil {
		t.Fatalf("BindNode(%v) failed: %v", address, err)
	}
	if err := nmClient.Update(rt.R().NewContext()); err != nil {
		t.Fatalf("%v.Update() failed: %v", address, err)
	}
}

// nodeManager is an enclosure for the node manager blackbox process.
func nodeManager(argv []string) {
	// The node manager program logic assumes that its binary is
	// executed through a symlink. To meet this assumption, the first
	// time this binary is started it creates a symlink to itself and
	// restarts itself using this symlink.
	fi, err := os.Lstat(os.Args[0])
	if err != nil {
		vlog.Fatalf("Lstat(%v) failed: %v", os.Args[0], err)
	}
	if fi.Mode()&os.ModeSymlink != os.ModeSymlink && os.Getenv(TEST_UPDATE_ENV) == "" {
		symlink := os.Args[0] + ".symlink"
		if err := os.Symlink(os.Args[0], symlink); err != nil {
			vlog.Fatalf("Symlink(%v, %v) failed: %v", os.Args[0], symlink, err)
		}
		argv := append([]string{symlink}, os.Args[1:]...)
		envv := os.Environ()
		if err := syscall.Exec(symlink, argv, envv); err != nil {
			vlog.Fatalf("Exec(%v, %v, %v) failed: %v", symlink, argv, envv, err)
		}
	} else {
		runtime := rt.Init()
		defer runtime.Shutdown()
		address, nmCleanup := startNodeManager(runtime, argv[0], argv[1])
		defer nmCleanup()
		invokeCallback(address)
		// Wait until shutdown.
		<-signals.ShutdownOnSignals()
		blackbox.WaitForEOFOnStdin()
	}
}

func spawnNodeManager(t *testing.T, mtAddress string, idFile string) *blackbox.Child {
	child := blackbox.HelperCommand(t, "nodeManager", mtAddress, idFile)
	child.Cmd.Env = exec.SetEnv(child.Cmd.Env, "MOUNTTABLE_ROOT", mtAddress)
	child.Cmd.Env = exec.SetEnv(child.Cmd.Env, "VEYRON_IDENTITY", idFile)
	child.Cmd.Env = exec.SetEnv(child.Cmd.Env, "VEYRON_NM_ROOT", os.TempDir())
	if err := child.Cmd.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	return child
}

func startMountTable(t *testing.T, runtime veyron2.Runtime) (string, func()) {
	server, err := runtime.NewServer(veyron2.ServesMountTableOpt(true))
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

func startNodeManager(runtime veyron2.Runtime, mtAddress, idFile string) (string, func()) {
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer() failed: %v", err)
	}
	protocol, hostname := "tcp", "localhost:0"
	endpoint, err := server.Listen(protocol, hostname)
	if err != nil {
		vlog.Fatalf("Listen(%v, %v) failed: %v", protocol, hostname, err)
	}
	envelope := &application.Envelope{}
	envelope.Args = make([]string, 0)
	envelope.Args = os.Args[1:]
	envelope.Env = make([]string, 0)
	envelope.Env = append(envelope.Env, os.Environ()...)
	suffix, crSuffix, arSuffix := "", "cr", "ar"
	name := naming.MakeTerminal(naming.JoinAddressName(endpoint.String(), suffix))
	vlog.VI(1).Infof("Node manager name: %v", name)
	dispatcher, crDispatcher, arDispatcher := NewDispatchers(nil, envelope, name)
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
	}
	if err := server.Register(crSuffix, crDispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", crSuffix, crDispatcher, err)
	}
	if err := server.Register(arSuffix, arDispatcher); err != nil {
		vlog.Fatalf("Register(%v, %v) failed: %v", arSuffix, arDispatcher, err)
	}
	publishAs := "nm"
	if err := server.Publish(publishAs); err != nil {
		vlog.Fatalf("Publish(%v) failed: %v", publishAs, err)
	}
	os.Setenv(ORIGIN_ENV, naming.JoinAddressName(name, "ar"))
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

func TestUpdate(t *testing.T) {
	// Set up a mount table.
	runtime := rt.Init()
	defer runtime.Shutdown()
	mtName, mtCleanup := startMountTable(t, runtime)
	defer mtCleanup()
	mt := runtime.MountTable()
	// The local, client side MountTable is now relative to the
	// MountTable server started above.
	mt.SetRoots([]string{mtName})
	ctx := runtime.NewContext()

	// Spawn a node manager with an identity blessed by the mounttable's
	// identity under the name "test", and obtain its address.
	//
	// TODO(ataly): Eventually we want to use the same identity the node
	// manager would have if it was running in production.
	idFile := testutil.SaveIdentityToFile(testutil.NewBlessedIdentity(runtime.Identity(), "test"))
	defer os.Remove(idFile)
	child := spawnNodeManager(t, mtName, idFile)
	defer child.Cleanup()
	child.Expect("ready")
	name := naming.Join(mtName, "nm")
	results, err := mt.Resolve(ctx, name)
	if err != nil {
		t.Fatalf("Resolve(%v) failed: %v", name, err)
	}
	if expected, got := 1, len(results); expected != got {
		t.Fatalf("Unexpected number of results: expected %d, got %d", expected, got)
	}
	nmAddress := results[0]
	vlog.VI(1).Infof("Node manager name: %v address: %v", name, nmAddress)

	// Invoke the Update method and check that another instance of the
	// node manager binary has been started.
	invokeUpdate(t, nmAddress)
	child.Expect("ready")
}

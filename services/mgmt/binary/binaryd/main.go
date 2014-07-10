package main

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"

	"veyron/lib/signals"
	vflag "veyron/security/flag"

	"veyron/services/mgmt/binary/impl"

	"veyron2/rt"
	"veyron2/vlog"
)

const (
	defaultDepth      = 3
	defaultRootPrefix = "veyron_binary_repository"
)

func main() {
	var address, protocol, name, root string
	// TODO(rthellend): Remove the address and protocol flags when the config manager is working.
	flag.StringVar(&address, "address", "localhost:0", "network address to listen on")
	flag.StringVar(&name, "name", "", "name to mount the binary repository as")
	flag.StringVar(&protocol, "protocol", "tcp", "network type to listen on")
	flag.StringVar(&root, "root", "", "root directory for the binary repository")
	flag.Parse()
	if root == "" {
		var err error
		if root, err = ioutil.TempDir("", defaultRootPrefix); err != nil {
			vlog.Errorf("TempDir() failed: %v\n", err)
			return
		}
		path, perm := filepath.Join(root, impl.VersionFile), os.FileMode(0600)
		if err := ioutil.WriteFile(path, []byte(impl.Version), perm); err != nil {
			vlog.Errorf("WriteFile(%v, %v, %v) failed: %v", path, impl.Version, perm, err)
			return
		}
	} else {
		_, err := os.Stat(root)
		switch {
		case err == nil:
		case os.IsNotExist(err):
			perm := os.FileMode(0700)
			if err := os.MkdirAll(root, perm); err != nil {
				vlog.Errorf("MkdirAll(%v, %v) failed: %v", root, perm, err)
				return
			}
			path, perm := filepath.Join(root, impl.VersionFile), os.FileMode(0600)
			if err := ioutil.WriteFile(path, []byte(impl.Version), perm); err != nil {
				vlog.Errorf("WriteFile(%v, %v, %v) failed: %v", path, impl.Version, perm, err)
				return
			}
		default:
			vlog.Errorf("Stat(%v) failed: %v", root, err)
			return
		}
	}
	vlog.Infof("Binary repository rooted at %v", root)
	runtime := rt.Init()
	defer runtime.Cleanup()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	auth := vflag.NewAuthorizerOrDie()
	dispatcher, err := impl.NewDispatcher(root, defaultDepth, auth)
	if err != nil {
		vlog.Errorf("NewDispatcher(%v, %v, %v) failed: %v", root, defaultDepth, auth, err)
		return
	}
	endpoint, err := server.Listen(protocol, address)
	if err != nil {
		vlog.Errorf("Listen(%v, %v) failed: %v", protocol, address, err)
		return
	}
	if err := server.Serve(name, dispatcher); err != nil {
		vlog.Errorf("Server(%v) failed: %v", name, err)
		return
	}
	vlog.Infof("Binary repository published at %v/%v", endpoint, name)
	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}

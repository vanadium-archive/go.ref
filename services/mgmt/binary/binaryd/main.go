package main

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	vflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron/services/mgmt/binary/impl"
)

const (
	defaultDepth      = 3
	defaultRootPrefix = "veyron_binary_repository"
)

var (
	name = flag.String("name", "", "name to mount the binary repository as")
	root = flag.String("root", "", "root directory for the binary repository")
)

func main() {
	flag.Parse()
	if *root == "" {
		var err error
		if *root, err = ioutil.TempDir("", defaultRootPrefix); err != nil {
			vlog.Errorf("TempDir() failed: %v\n", err)
			return
		}
		path, perm := filepath.Join(*root, impl.VersionFile), os.FileMode(0600)
		if err := ioutil.WriteFile(path, []byte(impl.Version), perm); err != nil {
			vlog.Errorf("WriteFile(%v, %v, %v) failed: %v", path, impl.Version, perm, err)
			return
		}
	} else {
		_, err := os.Stat(*root)
		switch {
		case err == nil:
		case os.IsNotExist(err):
			perm := os.FileMode(0700)
			if err := os.MkdirAll(*root, perm); err != nil {
				vlog.Errorf("MkdirAll(%v, %v) failed: %v", *root, perm, err)
				return
			}
			path, perm := filepath.Join(*root, impl.VersionFile), os.FileMode(0600)
			if err := ioutil.WriteFile(path, []byte(impl.Version), perm); err != nil {
				vlog.Errorf("WriteFile(%v, %v, %v) failed: %v", path, impl.Version, perm, err)
				return
			}
		default:
			vlog.Errorf("Stat(%v) failed: %v", *root, err)
			return
		}
	}
	vlog.Infof("Binary repository rooted at %v", *root)
	runtime := rt.Init()
	defer runtime.Cleanup()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	auth := vflag.NewAuthorizerOrDie()
	dispatcher, err := impl.NewDispatcher(*root, defaultDepth, auth)
	if err != nil {
		vlog.Errorf("NewDispatcher(%v, %v, %v) failed: %v", *root, defaultDepth, auth, err)
		return
	}
	endpoint, err := server.ListenX(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", roaming.ListenSpec, err)
		return
	}
	if err := server.Serve(*name, dispatcher); err != nil {
		vlog.Errorf("Serve(%v) failed: %v", *name, err)
		return
	}
	vlog.Infof("Binary repository running at endpoint=%q", endpoint)
	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}

package main

import (
	"flag"
	"io/ioutil"
	"os"

	"veyron/lib/signals"
	vflag "veyron/security/flag"

	"veyron/services/mgmt/content/impl"

	"veyron2/rt"
	"veyron2/vlog"
)

const (
	defaultDepth      = 3
	defaultRootPrefix = "veyron_content_manager"
)

func main() {
	var address, protocol, name, root string
	flag.StringVar(&address, "address", "localhost:0", "network address to listen on")
	flag.StringVar(&name, "name", "", "name to mount the content manager as")
	flag.StringVar(&protocol, "protocol", "tcp", "network type to listen on")
	flag.StringVar(&root, "root", "", "root directory for the content server")
	flag.Parse()
	if root == "" {
		var err error
		if root, err = ioutil.TempDir("", defaultRootPrefix); err != nil {
			vlog.Errorf("TempDir() failed: %v\n", err)
			return
		}
	} else {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			if err := os.MkdirAll(root, 0700); err != nil {
				vlog.Errorf("Directory %v does not exist and cannot be created.", root)
				return
			}
		}
	}
	vlog.VI(0).Infof("Content manager mounted at %v", root)
	runtime := rt.Init()
	defer runtime.Shutdown()
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()

	dispatcher := impl.NewDispatcher(root, defaultDepth, vflag.NewAuthorizerOrDie())
	suffix := ""
	if err := server.Register(suffix, dispatcher); err != nil {
		vlog.Errorf("Register(%v, %v) failed: %v", suffix, dispatcher, err)
		return
	}
	endpoint, err := server.Listen(protocol, address)
	if err != nil {
		vlog.Errorf("Listen(%v, %v) failed: %v", protocol, address, err)
		return
	}
	if err := server.Publish(name); err != nil {
		vlog.Errorf("Publish(%v) failed: %v", name, err)
		return
	}
	vlog.VI(0).Infof("Content manager published at %v/%v", endpoint, name)

	// Wait until shutdown.
	<-signals.ShutdownOnSignals()
}

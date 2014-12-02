package main

import (
	"flag"
	"net"
	"net/http"
	"os"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/netstate"
	"veyron.io/veyron/veyron/lib/signals"
	"veyron.io/veyron/veyron/profiles/roaming"
	vflag "veyron.io/veyron/veyron/security/flag"
	"veyron.io/veyron/veyron/services/mgmt/binary/impl"
)

const defaultDepth = 3

var (
	name     = flag.String("name", "", "name to mount the binary repository as")
	rootFlag = flag.String("root", "", "root directory for the binary repository")
	httpAddr = flag.String("http", ":0", "TCP address on which the HTTP server runs")
)

// toIPPort tries to swap in the 'best' accessible IP for the host part of the
// address, if the provided address has an unspecified IP.
func toIPPort(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		vlog.Errorf("SplitHostPort(%v) failed: %v", addr, err)
		os.Exit(1)
	}
	ip := net.ParseIP(host)
	if ip.IsUnspecified() {
		host = "127.0.0.1"
		ips, err := netstate.GetAccessibleIPs()
		if err == nil {
			if a, err := roaming.ListenSpec.AddressChooser("tcp", ips); err == nil && len(a) > 0 {
				host = a[0].Address().String()
			}
		}
	}
	return net.JoinHostPort(host, port)
}

func main() {
	runtime := rt.Init()
	defer runtime.Cleanup()
	root, err := impl.SetupRoot(*rootFlag)
	if err != nil {
		vlog.Errorf("SetupRoot(%q) failed: %v", *rootFlag, err)
		return
	}
	vlog.Infof("Binary repository rooted at %v", root)

	state, err := impl.NewState(root, defaultDepth)
	if err != nil {
		vlog.Errorf("NewState(%v, %v) failed: %v", root, defaultDepth, err)
		return
	}

	listener, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", *httpAddr, err)
		os.Exit(1)
	}
	vlog.Infof("Binary repository HTTP server at: %q", toIPPort(listener.Addr().String()))
	go func() {
		if err := http.Serve(listener, http.FileServer(impl.NewHTTPRoot(state))); err != nil {
			vlog.Errorf("Serve() failed: %v", err)
			os.Exit(1)
		}
	}()

	server, err := runtime.NewServer()
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	auth := vflag.NewAuthorizerOrDie()
	endpoint, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", roaming.ListenSpec, err)
		return
	}
	if err := server.ServeDispatcher(*name, impl.NewDispatcher(state, auth)); err != nil {
		vlog.Errorf("ServeDispatcher(%v) failed: %v", *name, err)
		return
	}
	epName := naming.JoinAddressName(endpoint.String(), "")
	if *name != "" {
		vlog.Infof("Binary repository serving at %q (%q)", *name, epName)
	} else {
		vlog.Infof("Binary repository serving at %q", epName)
	}
	// Wait until shutdown.
	<-signals.ShutdownOnSignals(runtime)
}

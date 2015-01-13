package main

import (
	"flag"
	"net"
	"net/http"
	"os"

	"v.io/core/veyron2"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/lib/netstate"
	"v.io/core/veyron/lib/signals"
	"v.io/core/veyron/profiles/roaming"
	vflag "v.io/core/veyron/security/flag"
	"v.io/core/veyron/services/mgmt/binary/impl"
)

const defaultDepth = 3

var (
	name        = flag.String("name", "", "name to mount the binary repository as")
	rootDirFlag = flag.String("root_dir", "", "root directory for the binary repository")
	httpAddr    = flag.String("http", ":0", "TCP address on which the HTTP server runs")
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
	runtime, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %v", err)
	}
	defer runtime.Cleanup()

	ctx := runtime.NewContext()

	rootDir, err := impl.SetupRootDir(*rootDirFlag)
	if err != nil {
		vlog.Errorf("SetupRootDir(%q) failed: %v", *rootDirFlag, err)
		return
	}
	vlog.Infof("Binary repository rooted at %v", rootDir)

	listener, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", *httpAddr, err)
		os.Exit(1)
	}
	rootURL := toIPPort(listener.Addr().String())
	state, err := impl.NewState(rootDir, rootURL, defaultDepth)
	if err != nil {
		vlog.Errorf("NewState(%v, %v, %v) failed: %v", rootDir, rootURL, defaultDepth, err)
		return
	}
	vlog.Infof("Binary repository HTTP server at: %q", rootURL)
	go func() {
		if err := http.Serve(listener, http.FileServer(impl.NewHTTPRoot(state))); err != nil {
			vlog.Errorf("Serve() failed: %v", err)
			os.Exit(1)
		}
	}()
	server, err := veyron2.NewServer(ctx)
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	auth := vflag.NewAuthorizerOrDie()
	endpoints, err := server.Listen(roaming.ListenSpec)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", roaming.ListenSpec, err)
		return
	}
	if err := server.ServeDispatcher(*name, impl.NewDispatcher(state, auth)); err != nil {
		vlog.Errorf("ServeDispatcher(%v) failed: %v", *name, err)
		return
	}
	epName := naming.JoinAddressName(endpoints[0].String(), "")
	if *name != "" {
		vlog.Infof("Binary repository serving at %q (%q)", *name, epName)
	} else {
		vlog.Infof("Binary repository serving at %q", epName)
	}
	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
}

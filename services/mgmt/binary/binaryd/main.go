// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"net"
	"net/http"
	"os"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/x/lib/vlog"

	"v.io/x/lib/netstate"
	"v.io/x/ref/lib/signals"
	_ "v.io/x/ref/profiles/roaming"
	"v.io/x/ref/services/mgmt/binary/impl"
)

const defaultDepth = 3

var (
	name        = flag.String("name", "", "name to mount the binary repository as")
	rootDirFlag = flag.String("root_dir", "", "root directory for the binary repository")
	httpAddr    = flag.String("http", ":0", "TCP address on which the HTTP server runs")
)

// toIPPort tries to swap in the 'best' accessible IP for the host part of the
// address, if the provided address has an unspecified IP.
func toIPPort(ctx *context.T, addr string) string {
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
			ls := v23.GetListenSpec(ctx)
			if a, err := ls.AddressChooser("tcp", ips); err == nil && len(a) > 0 {
				host = a[0].Address().String()
			}
		}
	}
	return net.JoinHostPort(host, port)
}

func main() {
	ctx, shutdown := v23.Init()
	defer shutdown()

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
	rootURL := toIPPort(ctx, listener.Addr().String())
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
	server, err := v23.NewServer(ctx)
	if err != nil {
		vlog.Errorf("NewServer() failed: %v", err)
		return
	}
	defer server.Stop()
	ls := v23.GetListenSpec(ctx)
	endpoints, err := server.Listen(ls)
	if err != nil {
		vlog.Errorf("Listen(%s) failed: %v", ls, err)
		return
	}

	dis, err := impl.NewDispatcher(v23.GetPrincipal(ctx), state)
	if err != nil {
		vlog.Errorf("NewDispatcher() failed: %v\n", err)
		return
	}
	if err := server.ServeDispatcher(*name, dis); err != nil {
		vlog.Errorf("ServeDispatcher(%v) failed: %v", *name, err)
		return
	}
	epName := endpoints[0].Name()
	if *name != "" {
		vlog.Infof("Binary repository serving at %q (%q)", *name, epName)
	} else {
		vlog.Infof("Binary repository serving at %q", epName)
	}
	// Wait until shutdown.
	<-signals.ShutdownOnSignals(ctx)
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mounttablelib

import (
	"net"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/x/lib/vlog"
)

func StartServers(ctx *context.T, listenSpec rpc.ListenSpec, mountName, nhName, permsFile, persistDir, debugPrefix string) (string, func(), error) {
	var stopFuncs []func() error
	stop := func() {
		for i := len(stopFuncs) - 1; i >= 0; i-- {
			stopFuncs[i]()
		}
	}

	mtServer, err := v23.NewServer(ctx, options.ServesMountTable(true))
	if err != nil {
		vlog.Errorf("v23.NewServer failed: %v", err)
		return "", nil, err
	}
	stopFuncs = append(stopFuncs, mtServer.Stop)
	mt, err := NewMountTableDispatcher(permsFile, persistDir, debugPrefix)
	if err != nil {
		vlog.Errorf("NewMountTable failed: %v", err)
		stop()
		return "", nil, err
	}
	mtEndpoints, err := mtServer.Listen(listenSpec)
	if err != nil {
		vlog.Errorf("mtServer.Listen failed: %v", err)
		stop()
		return "", nil, err
	}
	mtEndpoint := mtEndpoints[0]
	if err := mtServer.ServeDispatcher(mountName, mt); err != nil {
		vlog.Errorf("ServeDispatcher() failed: %v", err)
		stop()
		return "", nil, err
	}

	mtName := mtEndpoint.Name()
	vlog.Infof("Mount table service at: %q endpoint: %s", mountName, mtName)

	if len(nhName) > 0 {
		neighborhoodListenSpec := listenSpec.Copy()
		// The ListenSpec code ensures that we have a valid address here.
		host, port, _ := net.SplitHostPort(listenSpec.Addrs[0].Address)
		if port != "" {
			neighborhoodListenSpec.Addrs[0].Address = net.JoinHostPort(host, "0")
		}
		nhServer, err := v23.NewServer(ctx, options.ServesMountTable(true))
		if err != nil {
			vlog.Errorf("v23.NewServer failed: %v", err)
			stop()
			return "", nil, err
		}
		stopFuncs = append(stopFuncs, nhServer.Stop)
		if _, err := nhServer.Listen(neighborhoodListenSpec); err != nil {
			vlog.Errorf("nhServer.Listen failed: %v", err)
			stop()
			return "", nil, err
		}

		addresses := []string{}
		for _, ep := range mtEndpoints {
			addresses = append(addresses, ep.Name())
		}
		var nh rpc.Dispatcher
		if host == "127.0.0.1" || host == "localhost" {
			nh, err = NewLoopbackNeighborhoodDispatcher(nhName, addresses...)
		} else {
			nh, err = NewNeighborhoodDispatcher(nhName, addresses...)
		}
		if err != nil {
			vlog.Errorf("NewNeighborhoodServer failed: %v", err)
			stop()
			return "", nil, err
		}
		if err := nhServer.ServeDispatcher(naming.JoinAddressName(mtName, "nh"), nh); err != nil {
			vlog.Errorf("nhServer.ServeDispatcher failed to register neighborhood: %v", err)
			stop()
			return "", nil, err
		}
	}
	return mtName, stop, nil
}

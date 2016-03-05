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
)

func StartServers(ctx *context.T, listenSpec rpc.ListenSpec, mountName, nhName, permsFile, persistDir, debugPrefix string) (string, func(), error) {
	var stopFuncs []func() error
	stop := func() {
		for i := len(stopFuncs) - 1; i >= 0; i-- {
			stopFuncs[i]()
		}
	}

	mt, err := NewMountTableDispatcher(ctx, permsFile, persistDir, debugPrefix)
	if err != nil {
		ctx.Errorf("NewMountTable failed: %v", err)
		return "", nil, err
	}
	ctx = v23.WithListenSpec(ctx, listenSpec)
	ctx, mtServer, err := v23.WithNewDispatchingServer(ctx, mountName, mt, options.ServesMountTable(true))
	if err != nil {

		ctx.Errorf("v23.WithNewServer failed: %v", err)
		return "", nil, err
	}
	stopFuncs = append(stopFuncs, mtServer.Stop)
	var mtName string
	var mtEndpoints []naming.Endpoint
	for {
		status := mtServer.Status()
		mtEndpoints = status.Endpoints
		mtName = mtEndpoints[0].Name()
		if mtEndpoints[0].Addr().Network() != "bidi" {
			break
		}
		<-status.Valid
	}
	ctx.Infof("Mount table service at: %q endpoint: %s", mountName, mtName)

	if len(nhName) > 0 {
		// The ListenSpec code ensures that we have a valid address here.
		host, port, _ := net.SplitHostPort(listenSpec.Addrs[0].Address)
		if port != "" {
			neighborhoodListenSpec := listenSpec.Copy()
			neighborhoodListenSpec.Addrs[0].Address = net.JoinHostPort(host, "0")
			ctx = v23.WithListenSpec(ctx, neighborhoodListenSpec)
		}

		names := []string{}
		for _, ep := range mtEndpoints {
			names = append(names, ep.Name())
		}
		var nh rpc.Dispatcher
		if host == "127.0.0.1" || host == "localhost" {
			nh, err = NewLoopbackNeighborhoodDispatcher(nhName, names...)
		} else {
			nh, err = NewNeighborhoodDispatcher(nhName, names...)
		}

		ctx, nhServer, err := v23.WithNewDispatchingServer(ctx, naming.Join(mtName, "nh"), nh, options.ServesMountTable(true))
		if err != nil {
			ctx.Errorf("v23.WithNewServer failed: %v", err)
			stop()
			return "", nil, err
		}
		stopFuncs = append(stopFuncs, nhServer.Stop)
	}
	return mtName, stop, nil
}

package mounttable

import (
	"net"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/vlog"
)

func StartServers(ctx *context.T, listenSpec ipc.ListenSpec, mountName, nhName, aclFile string) (string, func(), error) {
	var stopFuncs []func() error
	stop := func() {
		for i := len(stopFuncs) - 1; i >= 0; i-- {
			stopFuncs[i]()
		}
	}

	mtServer, err := veyron2.NewServer(ctx, options.ServesMountTable(true))
	if err != nil {
		vlog.Errorf("veyron2.NewServer failed: %v", err)
		return "", nil, err
	}
	stopFuncs = append(stopFuncs, mtServer.Stop)
	mt, err := NewMountTableDispatcher(aclFile)
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
		neighborhoodListenSpec := listenSpec
		// The ListenSpec code ensures that we have a valid address here.
		host, port, _ := net.SplitHostPort(listenSpec.Addrs[0].Address)
		if port != "" {
			neighborhoodListenSpec.Addrs[0].Address = net.JoinHostPort(host, "0")
		}
		nhServer, err := veyron2.NewServer(ctx, options.ServesMountTable(true))
		if err != nil {
			vlog.Errorf("veyron2.NewServer failed: %v", err)
			stop()
			return "", nil, err
		}
		stopFuncs = append(stopFuncs, nhServer.Stop)
		if _, err := nhServer.Listen(neighborhoodListenSpec); err != nil {
			vlog.Errorf("nhServer.Listen failed: %v", err)
			stop()
			return "", nil, err
		}

		nh, err := NewLoopbackNeighborhoodDispatcher(nhName, mtName)
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

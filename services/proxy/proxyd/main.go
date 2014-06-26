// proxyd is a daemon that listens for connections from veyron services
// (typically behind NATs) and proxies these services to the outside world.
package main

import (
	"flag"
	"net/http"
	_ "net/http/pprof"
	"time"

	"veyron/runtimes/google/ipc/stream/proxy"
	"veyron/runtimes/google/lib/publisher"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

func main() {
	var (
		// TODO(rthellend): Remove the address and protocol flags when the config manager is working.
		address    = flag.String("address", ":0", "Network address the proxy listens on")
		pubAddress = flag.String("published_address", "", "Network address the proxy publishes. If empty, the value of --address will be used")
		protocol   = flag.String("protocol", "tcp", "Network type the proxy listens on")
		httpAddr   = flag.String("http", ":14142", "Network address on which the HTTP debug server runs")
		name       = flag.String("name", "", "Name to mount the proxy as")
	)

	r := rt.Init()
	defer r.Shutdown()

	rid, err := naming.NewRoutingID()
	if err != nil {
		vlog.Fatal(err)
	}

	proxy, err := proxy.New(rid, nil, *protocol, *address, *pubAddress)
	if err != nil {
		vlog.Fatal(err)
	}
	defer proxy.Shutdown()

	if len(*name) > 0 {
		publisher := publisher.New(r.NewContext(), r.Namespace(), time.Minute)
		defer publisher.WaitForStop()
		defer publisher.Stop()
		publisher.AddServer(naming.JoinAddressName(proxy.Endpoint().String(), ""))
		publisher.AddName(*name)
	}

	http.Handle("/", proxy)
	if err = http.ListenAndServe(*httpAddr, nil); err != nil {
		vlog.Fatal(err)
	}
}

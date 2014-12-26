// proxyd is a daemon that listens for connections from veyron services
// (typically behind NATs) and proxies these services to the outside world.
package main

import (
	"flag"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"time"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/rt"
	"v.io/core/veyron2/vlog"

	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron/runtimes/google/ipc/stream/proxy"
	"v.io/core/veyron/runtimes/google/lib/publisher"
)

var (
	// TODO(rthellend): Remove the protocol and address flags when the config
	// manager is working.
	protocol = flag.String("protocol", "tcp", "protocol to listen on")
	address  = flag.String("address", ":0", "address to listen on")

	pubAddress  = flag.String("published_address", "", "Network address the proxy publishes. If empty, the value of --address will be used")
	httpAddr    = flag.String("http", ":14142", "Network address on which the HTTP debug server runs")
	healthzAddr = flag.String("healthz_address", "", "Network address on which the HTTP healthz server runs. It is intended to be used with a load balancer. The load balancer must be able to reach this address in order to verify that the proxy server is running")
	name        = flag.String("name", "", "Name to mount the proxy as")
)

func main() {
	r, err := rt.New()
	if err != nil {
		vlog.Fatalf("Could not initialize runtime: %s", err)
	}
	defer r.Cleanup()

	rid, err := naming.NewRoutingID()
	if err != nil {
		vlog.Fatal(err)
	}
	proxy, err := proxy.New(rid, r.Principal(), *protocol, *address, *pubAddress)
	if err != nil {
		vlog.Fatal(err)
	}
	defer proxy.Shutdown()

	if len(*name) > 0 {
		publisher := publisher.New(r.NewContext(), r.Namespace(), time.Minute)
		defer publisher.WaitForStop()
		defer publisher.Stop()
		ep := naming.JoinAddressName(proxy.Endpoint().String(), "")
		publisher.AddServer(ep, false)
		// If the protocol is tcp we need to also publish the websocket endpoint.
		// TODO(bjornick): Remove this hack before we launch.
		if strings.HasPrefix(proxy.Endpoint().Addr().Network(), "tcp") {
			wsEP := strings.Replace(ep, "@"+proxy.Endpoint().Addr().Network()+"@", "@ws@", 1)
			publisher.AddServer(wsEP, false)
		}

		publisher.AddName(*name)
	}

	if len(*healthzAddr) != 0 {
		go startHealthzServer(*healthzAddr)
	}

	if len(*httpAddr) == 0 {
		select {}
	} else {
		http.Handle("/", proxy)
		if err = http.ListenAndServe(*httpAddr, nil); err != nil {
			vlog.Fatal(err)
		}
	}
}

// healthzHandler implements net/http.Handler
type healthzHandler struct{}

func (healthzHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

// startHealthzServer starts a HTTP server that simply returns "ok" to every
// request. This is needed to let the load balancer know that the proxy server
// is running.
func startHealthzServer(addr string) {
	s := http.Server{
		Addr:         addr,
		Handler:      healthzHandler{},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	if err := s.ListenAndServe(); err != nil {
		vlog.Fatal(err)
	}
}

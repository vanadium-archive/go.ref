package rt

import (
	"net"
	"net/http"
	"sync"
	// TODO(ashankar,cnicolaou): Remove net/http/pprof before "release"
	// since it installs default HTTP handlers.
	"net/http/pprof"

	"veyron/runtimes/google/ipc/stream/manager"

	"veyron2/ipc/stream"
	"veyron2/vlog"
)

var httpOnce sync.Once

func (rt *vrt) startHTTPDebugServerOnce() {
	httpOnce.Do(func() {
		if len(rt.httpServer) > 0 {
			rt.startHTTPDebugServer(rt.httpServer, rt.sm)
		}
	})
}

func (_ *vrt) startHTTPDebugServer(addr string, sm stream.Manager) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		vlog.Errorf("Failed to setup debugging HTTP server. net.Listen(%q, %q): %v", "tcp", addr, err)
		return
	}
	vlog.Infof("Starting HTTP debug server. See http://%v/debug/pprof and http://%v/debug/veyron", ln.Addr(), ln.Addr())
	mux := http.NewServeMux()
	if h, err := manager.HTTPHandler(sm); err != nil {
		vlog.Errorf("Failed to setup handler for ipc/stream/Manager implementation: %v", err)
	} else {
		mux.Handle("/debug/veyron/", h)
	}
	// Since a custom ServeMux is used, net/http/pprof.init needs to be replicated
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	go func() {
		server := &http.Server{Addr: ln.Addr().String(), Handler: mux}
		if err := server.Serve(ln); err != nil {
			vlog.Infof("Debug HTTP server serve exited: %v", err)
		}
	}()
}

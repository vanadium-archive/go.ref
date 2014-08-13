package rt

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
	// TODO(ashankar,cnicolaou): Remove net/http/pprof before "release"
	// since it installs default HTTP handlers.
	"net/http/pprof"

	"veyron/runtimes/google/ipc/stream/manager"
	"veyron2/ipc/stream"
	"veyron2/naming"
	"veyron2/vlog"
)

type debugServer struct {
	init sync.Once
	addr string
	mux  *http.ServeMux

	mu   sync.RWMutex
	rids []naming.RoutingID // GUARDED_BY(mu)
}

func (rt *vrt) initHTTPDebugServer() {
	// TODO(ashankar,cnicolaou): Change the default debug address to the empty
	// string.
	// In March 2014 this was temporarily set to "127.0.0.1:0" so that the
	// debugging HTTP server always runs, which was useful during initial veyron
	// development. We restrict it in this way to avoid annoying firewall warnings
	// and to provide a modicum of security.
	rt.debug.addr = "127.0.0.1:0"
	rt.debug.mux = http.NewServeMux()
}

func (rt *vrt) startHTTPDebugServerOnce() {
	rt.debug.init.Do(func() { startHTTPDebugServer(&rt.debug) })
}

func startHTTPDebugServer(info *debugServer) {
	if len(info.addr) == 0 {
		return
	}
	ln, err := net.Listen("tcp", info.addr)
	if err != nil {
		vlog.Errorf("Failed to setup debugging HTTP server. net.Listen(%q, %q): %v", "tcp", info.addr, err)
		return
	}
	vlog.Infof("Starting HTTP debug server. See http://%v/debug", ln.Addr())
	mux := info.mux
	mux.Handle("/debug/", info)
	// Since a custom ServeMux is used, net/http/pprof.init needs to be replicated
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	go func() {
		server := &http.Server{Addr: ln.Addr().String(), Handler: mux}
		if err := server.Serve(ln); err != nil {
			vlog.Infof("Debug HTTP server exited: %v", err)
		}
	}()
}

func (s *debugServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := tmplMain.Execute(w, s.rids); err != nil {
		vlog.Errorf("Error executing HTTP template: %v", err)
	}
}

func (s *debugServer) RegisterStreamManager(rid naming.RoutingID, sm stream.Manager) error {
	h, err := manager.HTTPHandler(sm)
	if err != nil {
		return fmt.Errorf("failed to setup HTTP handler for ipc/stream/Manager implementation: %v", err)
	}
	s.mu.Lock()
	s.rids = append(s.rids, rid)
	s.mu.Unlock()
	s.mux.Handle(fmt.Sprintf("/debug/veyron/%v", rid), h)
	return nil
}

var (
	tmplMain = template.Must(template.New("Debug").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Veyron Debug Server</title>
</head>
<body>
<ul>
<li><a href="/debug/pprof/">profiling</a></li>
{{range .}}
<li>ipc/stream/Manager for RoutingID <a href="/debug/veyron/{{.}}">{{.}}</a></li>
{{end}}
</ul>
</body>
</html>`))
)

package proxy

import (
	"fmt"
	"net/http"
)

// ServeHTTP implements the http.Handler interface and dumps out the routing
// table at the proxy in text format.
//
// The format is meant for debugging purposes and may change without notice.
//
// TODO(ashankar): Think about what, if anything, needs to be "hidden". Once
// the proxy authenticates itself to clients, then the proxy will be somewhat
// "secure" and routing information should not be leaked here. Exposed for now,
// because it is useful for initial debugging.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	write := func(s string) { w.Write([]byte(s)) }
	servers := p.servers.List()
	p.mu.RLock()
	defer p.mu.RUnlock()
	write(fmt.Sprintf("Proxy with endpoint: %q. #Processes:%d #Servers:%d\n", p.Endpoint(), len(p.processes), len(servers)))
	write("=========\n")
	write("PROCESSES\n")
	write("=========\n")
	index := 1
	for process, _ := range p.processes {
		write(fmt.Sprintf("(%d) - %v", index, process))
		index++
		process.mu.RLock()
		write(fmt.Sprintf(" NextVCI:%d #Severs:%d\n", process.nextVCI, len(process.servers)))
		for vci, d := range process.routingTable {
			write(fmt.Sprintf("    VCI %4d --> VCI %4d @ %s\n", vci, d.VCI, d.Process))
		}
		process.mu.RUnlock()
	}
	write("=======\n")
	write("SERVERS\n")
	write("=======\n")
	for ix, is := range servers {
		write(fmt.Sprintf("(%d) %v\n", ix+1, is))
	}
}

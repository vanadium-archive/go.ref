package benchmark

import (
	"io"
	"testing"

	_ "veyron.io/veyron/veyron/lib/tcp"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/manager"

	"veyron.io/veyron/veyron2/ipc/stream"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
)

const (
	// Shorthands
	securityNone = options.VCSecurityNone
	securityTLS  = options.VCSecurityConfidential
)

type listener struct {
	ln stream.Listener
	ep naming.Endpoint
}

// createListeners returns N (stream.Listener, naming.Endpoint) pairs, such
// that calling stream.Manager.Dial to each of the endpoints will end up
// creating a new VIF.
func createListeners(mode options.VCSecurityLevel, m stream.Manager, N int) (servers []listener, err error) {
	for i := 0; i < N; i++ {
		var l listener
		if l.ln, l.ep, err = m.Listen("tcp", "127.0.0.1:0", mode); err != nil {
			return
		}
		servers = append(servers, l)
	}
	return
}

func benchmarkFlow(b *testing.B, mode options.VCSecurityLevel, nVIFs, nVCsPerVIF, nFlowsPerVC int) {
	client := manager.InternalNew(naming.FixedRoutingID(0xcccccccc))
	server := manager.InternalNew(naming.FixedRoutingID(0x55555555))

	lns, err := createListeners(mode, server, nVIFs)
	if err != nil {
		b.Fatal(err)
	}

	nFlows := nVIFs * nVCsPerVIF * nFlowsPerVC
	rchan := make(chan io.ReadCloser, nFlows)
	wchan := make(chan io.WriteCloser, nFlows)

	go func() {
		defer close(wchan)
		for i := 0; i < nVIFs; i++ {
			ep := lns[i].ep
			for j := 0; j < nVCsPerVIF; j++ {
				vc, err := client.Dial(ep, mode)
				if err != nil {
					b.Error(err)
					return
				}
				for k := 0; k < nFlowsPerVC; k++ {
					flow, err := vc.Connect()
					if err != nil {
						b.Error(err)
						return
					}
					// Flows are "Accepted" by the remote
					// end only on the first Write.
					if _, err := flow.Write([]byte("hello")); err != nil {
						b.Error(err)
						return
					}
					wchan <- flow
				}
			}
		}
	}()

	go func() {
		defer close(rchan)
		for i := 0; i < nVIFs; i++ {
			ln := lns[i].ln
			nFlowsPerVIF := nVCsPerVIF * nFlowsPerVC
			for j := 0; j < nFlowsPerVIF; j++ {
				flow, err := ln.Accept()
				if err != nil {
					b.Error(err)
					return
				}
				rchan <- flow
			}
		}
	}()

	var readers []io.ReadCloser
	for r := range rchan {
		readers = append(readers, r)
	}
	var writers []io.WriteCloser
	for w := range wchan {
		writers = append(writers, w)
	}
	if b.Failed() {
		return
	}
	(&throughputTester{b: b, readers: readers, writers: writers}).Run()
}

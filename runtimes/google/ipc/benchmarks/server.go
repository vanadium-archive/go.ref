package benchmarks

import (
	sflag "veyron/security/flag"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/rt"
	"veyron2/vlog"
)

type impl struct {
}

func (i *impl) Echo(ctx ipc.ServerContext, payload []byte) ([]byte, error) {
	return payload, nil
}

func (i *impl) EchoStream(ctx ipc.ServerContext, stream BenchmarkServiceEchoStreamStream) error {
	for stream.Advance() {
		chunk := stream.Value()
		if err := stream.Send(chunk); err != nil {
			return err
		}
	}

	return stream.Err()
}

// StartServer starts a server that implements the Benchmark service. The
// server listens to the given protocol and address, and returns the veyron
// address of the server and a callback function to stop the server.
func StartServer(protocol, address string) (string, func()) {
	server, err := rt.R().NewServer()
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	ep, err := server.Listen(protocol, address)
	if err != nil {
		vlog.Fatalf("Listen failed: %v", err)
	}
	if err := server.Serve("", ipc.SoloDispatcher(NewServerBenchmark(&impl{}), sflag.NewAuthorizerOrDie())); err != nil {
		vlog.Fatalf("Serve failed: %v", err)
	}
	return naming.JoinAddressName(ep.String(), ""), func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

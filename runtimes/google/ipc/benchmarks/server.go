package benchmarks

import (
	sflag "veyron/security/flag"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/vlog"
)

type impl struct {
}

func (i *impl) Echo(ctx ipc.ServerContext, payload []byte) ([]byte, error) {
	return payload, nil
}

func (i *impl) EchoStream(ctx ipc.ServerContext, stream BenchmarkServiceEchoStreamStream) error {
	rStream := stream.RecvStream()
	sender := stream.SendStream()
	for rStream.Advance() {
		chunk := rStream.Value()
		if err := sender.Send(chunk); err != nil {
			return err
		}
	}

	return rStream.Err()
}

// StartServer starts a server that implements the Benchmark service. The
// server listens to the given protocol and address, and returns the veyron
// address of the server and a callback function to stop the server.
func StartServer(runtime veyron2.Runtime, protocol, address string) (string, func()) {
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	ep, err := server.Listen(protocol, address)
	if err != nil {
		vlog.Fatalf("Listen failed: %v", err)
	}
	if err := server.Serve("", ipc.LeafDispatcher(NewServerBenchmark(&impl{}), sflag.NewAuthorizerOrDie())); err != nil {
		vlog.Fatalf("Serve failed: %v", err)
	}
	return naming.JoinAddressName(ep.String(), ""), func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

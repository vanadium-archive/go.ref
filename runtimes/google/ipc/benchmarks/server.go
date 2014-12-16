package benchmarks

import (
	sflag "veyron.io/veyron/veyron/security/flag"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/vlog"
)

type impl struct {
}

func (i *impl) Echo(ctx ipc.ServerContext, payload []byte) ([]byte, error) {
	return payload, nil
}

func (i *impl) EchoStream(ctx BenchmarkEchoStreamContext) error {
	rStream := ctx.RecvStream()
	sStream := ctx.SendStream()
	for rStream.Advance() {
		sStream.Send(rStream.Value())
	}
	if err := rStream.Err(); err != nil {
		return err
	}
	return nil
}

// StartServer starts a server that implements the Benchmark service. The
// server listens to the given protocol and address, and returns the veyron
// address of the server and a callback function to stop the server.
func StartServer(runtime veyron2.Runtime, listenSpec ipc.ListenSpec) (string, func()) {
	server, err := runtime.NewServer()
	if err != nil {
		vlog.Fatalf("NewServer failed: %v", err)
	}
	eps, err := server.Listen(listenSpec)
	if err != nil {
		vlog.Fatalf("Listen failed: %v", err)
	}

	if err := server.Serve("", BenchmarkServer(&impl{}), sflag.NewAuthorizerOrDie()); err != nil {
		vlog.Fatalf("Serve failed: %v", err)
	}
	return naming.JoinAddressName(eps[0].String(), ""), func() {
		if err := server.Stop(); err != nil {
			vlog.Fatalf("Stop() failed: %v", err)
		}
	}
}

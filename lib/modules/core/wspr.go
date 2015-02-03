package core

import (
	"flag"
	"fmt"
	"io"

	"v.io/core/veyron/lib/modules"
	"v.io/wspr/veyron/services/wsprd/wspr"

	"v.io/core/veyron2"
)

var (
	port   *int    = flag.CommandLine.Int("port", 0, "Port to listen on.")
	identd *string = flag.CommandLine.String("identd", "", "identd server name. Must be set.")
)

func init() {
	modules.RegisterChild(WSPRCommand, usage(flag.CommandLine), startWSPR)
}

func startWSPR(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	l := veyron2.GetListenSpec(ctx)
	proxy := wspr.NewWSPR(ctx, *port, &l, *identd, nil)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	fmt.Fprintf(stdout, "WSPR_ADDR=%s\n", addr)
	modules.WaitForEOF(stdin)
	return nil
}

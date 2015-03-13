package core

import (
	"flag"
	"fmt"
	"io"

	"v.io/x/ref/services/wsprd/wspr"
	"v.io/x/ref/test/modules"

	"v.io/v23"
)

var (
	port   *int    = flag.CommandLine.Int("port", 0, "Port to listen on.")
	identd *string = flag.CommandLine.String("identd", "", "identd server name. Must be set.")
)

func init() {
	modules.RegisterChild(WSPRCommand, usage(flag.CommandLine), startWSPR)
}

func startWSPR(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	l := v23.GetListenSpec(ctx)
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

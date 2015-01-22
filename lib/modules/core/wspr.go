package core

import (
	"flag"
	"fmt"
	"io"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/modules"
	"v.io/wspr/veyron/services/wsprd/wspr"

	"v.io/core/veyron2"
)

var (
	// TODO(sadovsky): We should restructure things so that we can avoid
	// duplicating code between subprocess command impls and actual main()'s.
	fs *flag.FlagSet = flag.NewFlagSet("wspr", flag.ContinueOnError)

	port   *int    = fs.Int("port", 0, "Port to listen on.")
	identd *string = fs.String("identd", "", "identd server name. Must be set.")

	fl *flags.Flags = flags.CreateAndRegister(fs, flags.Listen)
)

func init() {
	modules.RegisterChild(WSPRCommand, usage(fs), startWSPR)
}

func startWSPR(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if err := parseFlags(fl, args); err != nil {
		return fmt.Errorf("failed to parse args: %s", err)
	}

	ctx, shutdown := veyron2.Init()
	defer shutdown()

	l := initListenSpec(fl)
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

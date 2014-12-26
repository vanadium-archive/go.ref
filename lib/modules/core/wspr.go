package core

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/modules"
	"v.io/wspr/veyron/services/wsprd/wspr"

	"v.io/core/veyron2/rt"
)

var (
	// TODO(sadovsky): We should restructure things so that we can avoid
	// duplicating code between subprocess command impls and actual main()'s.
	fs *flag.FlagSet = flag.NewFlagSet("wspr", flag.ContinueOnError)

	port   *int    = fs.Int("port", 0, "Port to listen on.")
	identd *string = fs.String("identd", "", "identd server name. Must be set.")

	fl *flags.Flags = flags.CreateAndRegister(fs, flags.Listen)
)

func usageWSPR() string {
	res := []string{}
	fs.VisitAll(func(f *flag.Flag) {
		format := "  -%s=%s: %s"
		if getter, ok := f.Value.(flag.Getter); ok {
			if _, ok := getter.Get().(string); ok {
				// put quotes on the value
				format = "  -%s=%q: %s"
			}
		}
		res = append(res, fmt.Sprintf(format, f.Name, f.DefValue, f.Usage))
	})
	return strings.Join(res, "\n") + "\n"
}

func init() {
	modules.RegisterChild(WSPRCommand, usageWSPR(), startWSPR)
}

func startWSPR(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if err := parseFlags(fl, args); err != nil {
		return fmt.Errorf("failed to parse args: %s", err)
	}
	args = fl.Args()

	r, err := rt.New()
	if err != nil {
		return fmt.Errorf("rt.New failed: %s", err)
	}
	defer r.Cleanup()
	l := initListenSpec(fl)
	proxy := wspr.NewWSPR(r, *port, nil, &l, *identd, nil)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	fmt.Fprintf(stdout, "WSPR_ADDR=%s\n", addr.String())
	modules.WaitForEOF(stdin)
	return nil
}

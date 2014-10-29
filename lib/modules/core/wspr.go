package core

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/options"

	"veyron.io/veyron/veyron/lib/flags"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/wspr/veyron/services/wsprd/wspr"
)

var (
	// TODO(sadovsky): We should restructure things so that we can avoid
	// duplicating code between subprocess command impls and actual main()'s.
	fs *flag.FlagSet = flag.NewFlagSet("wspr", flag.ContinueOnError)

	port   *int    = fs.Int("port", 0, "Port to listen on.")
	identd *string = fs.String("identd", "", "identd server name. Must be set.")
	// TODO(ataly, ashankar, bjornick): Remove this flag once the old security
	// model is killed.
	newSecurityModel *bool = fs.Bool("new_security_model", false, "Use the new security model.")

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

	var opts []veyron2.ROpt
	if *newSecurityModel {
		opts = append(opts, options.ForceNewSecurityModel{})
	}

	proxy := wspr.NewWSPR(*port, initListenSpec(fl), *identd)
	defer proxy.Shutdown()

	addr := proxy.Listen()
	go func() {
		proxy.Serve()
	}()

	fmt.Fprintf(stdout, "WSPR_ADDR=%s\n", addr.String())
	modules.WaitForEOF(stdin)
	return nil
}

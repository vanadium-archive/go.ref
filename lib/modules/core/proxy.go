package core

import (
	"fmt"
	"io"
	"os"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/naming"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/runtimes/google/ipc/stream/proxy"
	"v.io/core/veyron/runtimes/google/lib/publisher"
)

func init() {
	modules.RegisterChild(ProxyServerCommand, `<name>`, proxyServer)
}

func proxyServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := veyron2.Init()
	defer shutdown()

	fl, args, err := parseListenFlags(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %s", err)
	}
	expected := len(args)
	rid, err := naming.NewRoutingID()
	if err != nil {
		return err
	}
	lf := fl.ListenFlags()

	proxy, err := proxy.New(rid, veyron2.GetPrincipal(ctx), lf.Addrs[0].Protocol, lf.Addrs[0].Address, "")
	if err != nil {
		return err
	}
	defer proxy.Shutdown()

	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	if expected > 0 {
		pub := publisher.New(ctx, veyron2.GetNamespace(ctx), time.Minute)
		defer pub.WaitForStop()
		defer pub.Stop()
		pub.AddServer(proxy.Endpoint().String(), false)
		for _, name := range args {
			if len(name) == 0 {
				return fmt.Errorf("empty name specified on the command line")
			}
			pub.AddName(name)
		}
		// Wait for all the entries to be published.
		for {
			pubState := pub.Status()
			if expected == len(pubState) {
				break
			}
			fmt.Fprintf(stderr, "%s\n", pub.DebugString())
			delay := time.Second
			fmt.Fprintf(stderr, "Sleeping: %s\n", delay)
			time.Sleep(delay)
		}
	}
	fmt.Fprintf(stdout, "PROXY_NAME=%s\n", proxy.Endpoint().Name())
	modules.WaitForEOF(stdin)
	fmt.Fprintf(stdout, "DONE\n")
	return nil
}

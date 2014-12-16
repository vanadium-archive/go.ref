package core

import (
	"fmt"
	"io"
	"os"
	"time"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/runtimes/google/ipc/stream/proxy"
	"veyron.io/veyron/veyron/runtimes/google/lib/publisher"
)

func init() {
	modules.RegisterChild(ProxyServerCommand, `<name>`, proxyServer)
}

func proxyServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r, err := rt.New()
	if err != nil {
		panic(err)
	}
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

	proxy, err := proxy.New(rid, r.Principal(), lf.Addrs[0].Protocol, lf.Addrs[0].Address, "")
	if err != nil {
		return err
	}
	defer proxy.Shutdown()

	pname := naming.JoinAddressName(proxy.Endpoint().String(), "")
	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	fmt.Fprintf(stdout, "PROXY_ADDR=%s\n", proxy.Endpoint().String())
	fmt.Fprintf(stdout, "PROXY_NAME=%s\n", pname)
	if expected > 0 {
		defer r.Cleanup()
		pub := publisher.New(r.NewContext(), r.Namespace(), time.Minute)
		defer pub.WaitForStop()
		defer pub.Stop()
		pub.AddServer(pname, false)
		for _, name := range args {
			if len(name) == 0 {
				return fmt.Errorf("empty name specified on the command line")
			}
			pub.AddName(name)
		}
		// Wait for all the entries to be published.
		for {
			got := len(pub.Published())
			if expected == got {
				break
			}
			fmt.Fprintf(stderr, "%s\n", pub.DebugString())
			delay := time.Second
			fmt.Fprintf(stderr, "Sleeping: %s\n", delay)
			time.Sleep(delay)
		}
		for _, p := range pub.Published() {
			fmt.Fprintf(stdout, "PUBLISHED_PROXY_NAME=%s\n", p)
		}
	}
	modules.WaitForEOF(stdin)
	fmt.Fprintf(stdout, "DONE\n")
	return nil
}

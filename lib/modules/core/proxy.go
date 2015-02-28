package core

import (
	"fmt"
	"io"
	"os"
	"time"

	"v.io/v23"
	"v.io/v23/naming"

	"v.io/x/ref/lib/modules"
	"v.io/x/ref/runtimes/google/ipc/stream/proxy"
	"v.io/x/ref/runtimes/google/lib/publisher"
)

func init() {
	modules.RegisterChild(ProxyServerCommand, `<name>`, proxyServer)
}

func proxyServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	expected := len(args)
	rid, err := naming.NewRoutingID()
	if err != nil {
		return err
	}

	listenSpec := v23.GetListenSpec(ctx)
	protocol := listenSpec.Addrs[0].Protocol
	addr := listenSpec.Addrs[0].Address

	proxy, err := proxy.New(rid, v23.GetPrincipal(ctx), protocol, addr, "")
	if err != nil {
		return err
	}
	defer proxy.Shutdown()

	fmt.Fprintf(stdout, "PID=%d\n", os.Getpid())
	if expected > 0 {
		pub := publisher.New(ctx, v23.GetNamespace(ctx), time.Minute)
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

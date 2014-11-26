package core

import (
	"fmt"
	"io"
	"strings"
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
	fl, args, err := parseListenFlags(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %s", err)
	}
	// TODO(sadovsky): Why does this require >=1 arg? Seems 0 should be fine.
	// Also note, we have no way to specify ">=0".
	if err := checkArgs(args, -1, ""); err != nil {
		return err
	}
	expected := len(args)
	rid, err := naming.NewRoutingID()
	if err != nil {
		return err
	}
	lf := fl.ListenFlags()
	// TODO(ashankar): Set the second argument to r.Principal() once the
	// old security model is no longer operational.
	proxy, err := proxy.New(rid, nil, lf.ListenProtocol.String(), lf.ListenAddress.String(), "")
	if err != nil {
		return err
	}
	defer proxy.Shutdown()
	pname := naming.JoinAddressName(proxy.Endpoint().String(), "")
	fmt.Fprintf(stdout, "PROXY_ADDR=%s\n", proxy.Endpoint().String())
	fmt.Fprintf(stdout, "PROXY_NAME=%s\n", pname)
	r := rt.R()
	pub := publisher.New(r.NewContext(), r.Namespace(), time.Minute)
	defer pub.WaitForStop()
	defer pub.Stop()
	pub.AddServer(pname, false)
	// If the protocol is tcp we need to also publish the websocket endpoint.
	// TODO(bjornick): Remove this hack before we launch.
	if strings.HasPrefix(proxy.Endpoint().Addr().Network(), "tcp") {
		wsEP := strings.Replace(pname, "@"+proxy.Endpoint().Addr().Network()+"@", "@ws@", 1)
		pub.AddServer(wsEP, false)
	}
	for _, name := range args {
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
	modules.WaitForEOF(stdin)
	return nil
}

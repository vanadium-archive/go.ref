package core

import (
	"fmt"
	"io"
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
		return fmt.Errorf("failed parsing args: %s", err)
	} //	args = fl.Args()
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
	pname := naming.JoinAddressName(proxy.Endpoint().String(), "//")
	fmt.Fprintf(stdout, "PROXY_ADDR=%s\n", proxy.Endpoint().String())
	fmt.Fprintf(stdout, "PROXY_NAME=%s\n", pname)
	r := rt.R()
	pub := publisher.New(r.NewContext(), r.Namespace(), time.Minute)
	defer pub.WaitForStop()
	defer pub.Stop()
	pub.AddServer(pname, false)
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
	fmt.Fprintf(stdout, "READY")
	modules.WaitForEOF(stdin)
	return nil
}

package flags_test

import (
	"flag"
	"testing"

	"veyron.io/veyron/veyron/lib/flags"
)

func TestFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fl := flags.New(fs)

	addr := "192.168.10.1:0"
	roots := "ab:cd:ef"
	args := []string{"--veyron.tcp.address=" + addr, "--veyron.namespace.roots=" + roots}
	fs.Parse(args)
	if got, want := fl.NamespaceRootsFlag, roots; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := fl.ListenAddressFlag.String(), addr; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

package flags_test

import (
	"flag"
	"io/ioutil"
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
	if got, want := len(fl.Args()), 0; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestFlagError(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fl := flags.New(fs)
	addr := "192.168.10.1:0"
	args := []string{"--xxxveyron.tcp.address=" + addr, "not an arg"}
	err := fl.Parse(args)
	if err == nil {
		t.Fatalf("expected this to fail!")
	}
	if got, want := len(fl.Args()), 1; got != want {
		t.Errorf("got %d, want %d [args: %v]", got, want, fl.Args())
	}

}

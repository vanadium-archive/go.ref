package flags_test

import (
	"flag"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron/lib/flags"
	"veyron.io/veyron/veyron/lib/flags/consts"
)

func TestFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if flags.CreateAndRegister(fs) != nil {
		t.Fatalf("should have returned a nil value")
	}
	fl := flags.CreateAndRegister(fs, flags.Runtime)
	if fl == nil {
		t.Errorf("should have failed")
	}
	creds := "creddir"
	roots := []string{"ab:cd:ef"}
	args := []string{"--veyron.credentials=" + creds, "--veyron.namespace.root=" + roots[0]}
	fl.Parse(args)
	rtf := fl.RuntimeFlags()
	if got, want := rtf.NamespaceRoots, roots; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := rtf.Credentials, creds; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := fl.HasGroup(flags.Listen), false; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
	// Make sure we have a deep copy.
	rtf.NamespaceRoots[0] = "oooh"
	rtf = fl.RuntimeFlags()
	if got, want := rtf.NamespaceRoots, roots; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestACLFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fl := flags.CreateAndRegister(fs, flags.Runtime, flags.ACL)
	args := []string{"--veyron.acl=veyron:foo.json", "--veyron.acl=bar:bar.json", "--veyron.acl=baz:bar:baz.json"}
	fl.Parse(args)
	aclf := fl.ACLFlags()
	if got, want := aclf.ACLFile("veyron"), "foo.json"; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
	if got, want := aclf.ACLFile("bar"), "bar.json"; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
	if got, want := aclf.ACLFile("wombat"), ""; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
	if got, want := aclf.ACLFile("baz"), "bar:baz.json"; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
}

func TestFlagError(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fl := flags.CreateAndRegister(fs, flags.Runtime)
	addr := "192.168.10.1:0"
	args := []string{"--xxxveyron.tcp.address=" + addr, "not an arg"}
	err := fl.Parse(args)
	if err == nil {
		t.Fatalf("expected this to fail!")
	}
	if got, want := len(fl.Args()), 1; got != want {
		t.Errorf("got %d, want %d [args: %v]", got, want, fl.Args())
	}

	fs = flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fl = flags.CreateAndRegister(fs, flags.ACL)
	args = []string{"--veyron.acl=noname"}
	err = fl.Parse(args)
	if err == nil {
		t.Fatalf("expected this to fail!")
	}
}

func TestFlagsGroups(t *testing.T) {
	fl := flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Runtime, flags.Listen)
	if got, want := fl.HasGroup(flags.Listen), true; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
	addr := "192.168.10.1:0"
	roots := []string{"ab:cd:ef"}
	args := []string{"--veyron.tcp.address=" + addr, "--veyron.namespace.root=" + roots[0]}
	fl.Parse(args)
	lf := fl.ListenFlags()
	if got, want := fl.RuntimeFlags().NamespaceRoots, roots; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := lf.ListenAddress.String(), addr; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

const (
	rootEnvVar  = consts.NamespaceRootPrefix
	rootEnvVar0 = consts.NamespaceRootPrefix + "0"
)

func TestEnvVars(t *testing.T) {
	oldcreds := os.Getenv(consts.VeyronCredentials)
	defer os.Setenv(consts.VeyronCredentials, oldcreds)

	oldroot := os.Getenv(rootEnvVar)
	oldroot0 := os.Getenv(rootEnvVar0)
	defer os.Setenv(rootEnvVar, oldroot)
	defer os.Setenv(rootEnvVar0, oldroot0)

	os.Setenv(consts.VeyronCredentials, "bar")
	fl := flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Runtime)
	if err := fl.Parse([]string{}); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf := fl.RuntimeFlags()
	if got, want := rtf.Credentials, "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := fl.Parse([]string{"--veyron.credentials=baz"}); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf = fl.RuntimeFlags()
	if got, want := rtf.Credentials, "baz"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	os.Setenv(rootEnvVar, "a:1")
	os.Setenv(rootEnvVar0, "a:2")
	fl = flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Runtime)
	if err := fl.Parse([]string{}); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf = fl.RuntimeFlags()
	if got, want := rtf.NamespaceRoots, []string{"a:1", "a:2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := fl.Parse([]string{"--veyron.namespace.root=b:1", "--veyron.namespace.root=b:2", "--veyron.namespace.root=b:3", "--veyron.credentials=b:4"}); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf = fl.RuntimeFlags()
	if got, want := rtf.NamespaceRoots, []string{"b:1", "b:2", "b:3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := rtf.Credentials, "b:4"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaults(t *testing.T) {
	oldroot := os.Getenv(rootEnvVar)
	oldroot0 := os.Getenv(rootEnvVar0)
	defer os.Setenv(rootEnvVar, oldroot)
	defer os.Setenv(rootEnvVar0, oldroot0)

	os.Setenv(rootEnvVar, "")
	os.Setenv(rootEnvVar0, "")

	fl := flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Runtime, flags.ACL)
	if err := fl.Parse([]string{}); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf := fl.RuntimeFlags()
	if got, want := rtf.NamespaceRoots, []string{"/proxy.envyor.com:8101"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	aclf := fl.ACLFlags()
	if got, want := aclf.ACLFile("veyron"), "acl.json"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

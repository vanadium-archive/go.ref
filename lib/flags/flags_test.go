package flags_test

import (
	"flag"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"v.io/core/veyron/lib/flags"
	"v.io/core/veyron/lib/flags/consts"
)

func TestFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if flags.CreateAndRegister(fs) != nil {
		t.Fatalf("should have returned a nil value")
	}
	fl := flags.CreateAndRegister(fs, flags.Runtime)
	if fl == nil {
		t.Errorf("should have succeeded")
	}
	creds := "creddir"
	roots := []string{"ab:cd:ef"}
	args := []string{"--veyron.credentials=" + creds, "--veyron.namespace.root=" + roots[0]}
	fl.Parse(args, nil)
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
	args := []string{"--veyron.acl.file=runtime:foo.json", "--veyron.acl.file=bar:bar.json", "--veyron.acl.file=baz:bar:baz.json"}
	fl.Parse(args, nil)
	aclf := fl.ACLFlags()

	if got, want := aclf.ACLFile("runtime"), "foo.json"; got != want {
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

func TestACLLiteralFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fl := flags.CreateAndRegister(fs, flags.Runtime, flags.ACL)
	args := []string{"--veyron.acl.literal=hedgehog"}
	fl.Parse(args, nil)
	aclf := fl.ACLFlags()

	if got, want := aclf.ACLFile("runtime"), ""; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
	if got, want := aclf.ACLLiteral(), "hedgehog"; got != want {
		t.Errorf("got %t, want %t, ok %t", got, want)
	}
}

func TestACLLiteralBoth(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fl := flags.CreateAndRegister(fs, flags.Runtime, flags.ACL)
	args := []string{"--veyron.acl.file=runtime:foo.json", "--veyron.acl.literal=hedgehog"}
	fl.Parse(args, nil)
	aclf := fl.ACLFlags()

	if got, want := aclf.ACLFile("runtime"), "foo.json"; got != want {
		t.Errorf("got %t, want %t", got, want)
	}
	if got, want := aclf.ACLLiteral(), "hedgehog"; got != want {
		t.Errorf("got %t, want %t, ok %t", got, want)
	}
}

func TestFlagError(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(ioutil.Discard)
	fl := flags.CreateAndRegister(fs, flags.Runtime)
	addr := "192.168.10.1:0"
	args := []string{"--xxxveyron.tcp.address=" + addr, "not an arg"}
	err := fl.Parse(args, nil)
	if err == nil {
		t.Fatalf("expected this to fail!")
	}
	if got, want := len(fl.Args()), 1; got != want {
		t.Errorf("got %d, want %d [args: %v]", got, want, fl.Args())
	}

	fs = flag.NewFlagSet("test", flag.ContinueOnError)
	fl = flags.CreateAndRegister(fs, flags.ACL)
	args = []string{"--veyron.acl.file=noname"}
	err = fl.Parse(args, nil)
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
	fl.Parse(args, nil)
	lf := fl.ListenFlags()
	if got, want := fl.RuntimeFlags().NamespaceRoots, roots; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := lf.Addrs[0].Address, addr; got != want {
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
	if err := fl.Parse([]string{}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf := fl.RuntimeFlags()
	if got, want := rtf.Credentials, "bar"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := fl.Parse([]string{"--veyron.credentials=baz"}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf = fl.RuntimeFlags()
	if got, want := rtf.Credentials, "baz"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	os.Setenv(rootEnvVar, "a:1")
	os.Setenv(rootEnvVar0, "a:2")
	fl = flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Runtime)
	if err := fl.Parse([]string{}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf = fl.RuntimeFlags()
	if got, want := rtf.NamespaceRoots, []string{"a:1", "a:2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := fl.Parse([]string{"--veyron.namespace.root=b:1", "--veyron.namespace.root=b:2", "--veyron.namespace.root=b:3", "--veyron.credentials=b:4"}, nil); err != nil {
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
	if err := fl.Parse([]string{}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	rtf := fl.RuntimeFlags()
	if got, want := rtf.NamespaceRoots, []string{"/ns.dev.v.io:8101"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	aclf := fl.ACLFlags()
	if got, want := aclf.ACLFile(""), ""; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestListenFlags(t *testing.T) {
	fl := flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Listen)
	if err := fl.Parse([]string{}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	lf := fl.ListenFlags()
	if got, want := len(lf.Addrs), 1; got != want {
		t.Errorf("got %d, want %d", got, want)
	}

	// Test the default protocol and address is "wsh" and ":0".
	def := struct{ Protocol, Address string }{"wsh", ":0"}
	if got, want := lf.Addrs[0], def; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	fl = flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Listen)
	if err := fl.Parse([]string{
		"--veyron.tcp.address=172.0.0.1:10", // Will default to protocol "wsh".
		"--veyron.tcp.protocol=tcp", "--veyron.tcp.address=127.0.0.10:34",
		"--veyron.tcp.protocol=ws4", "--veyron.tcp.address=127.0.0.10:44",
		"--veyron.tcp.protocol=tcp6", "--veyron.tcp.address=172.0.0.100:100"}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	lf = fl.ListenFlags()
	if got, want := len(lf.Addrs), 4; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	for i, p := range []string{"wsh", "tcp", "ws4", "tcp6"} {
		if got, want := lf.Addrs[i].Protocol, p; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	for i, p := range []string{"172.0.0.1:10", "127.0.0.10:34", "127.0.0.10:44", "172.0.0.100:100"} {
		if got, want := lf.Addrs[i].Address, p; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestDuplicateFlags(t *testing.T) {
	fl := flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Listen)
	if err := fl.Parse([]string{
		"--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=172.0.0.1:34",
		"--veyron.tcp.protocol=tcp", "--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=172.0.0.1:34",
		"--veyron.tcp.protocol=ws", "--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=172.0.0.1:34", "--veyron.tcp.address=172.0.0.1:34"}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	lf := fl.ListenFlags()
	if got, want := len(lf.Addrs), 6; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	expected := flags.ListenAddrs{
		{"wsh", "172.0.0.1:10"},
		{"wsh", "172.0.0.1:34"},
		{"tcp", "172.0.0.1:10"},
		{"tcp", "172.0.0.1:34"},
		{"ws", "172.0.0.1:10"},
		{"ws", "172.0.0.1:34"},
	}
	if got, want := lf.Addrs, expected; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if err := fl.Parse([]string{
		"--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=172.0.0.1:34",
		"--veyron.tcp.protocol=tcp", "--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=127.0.0.1:34", "--veyron.tcp.address=127.0.0.1:34",
		"--veyron.tcp.protocol=ws", "--veyron.tcp.address=172.0.0.1:10", "--veyron.tcp.address=127.0.0.1:34", "--veyron.tcp.address=127.0.0.1:34"}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := len(lf.Addrs), 6; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if got, want := lf.Addrs, expected; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}

	fl = flags.CreateAndRegister(flag.NewFlagSet("test", flag.ContinueOnError), flags.Runtime)

	if err := fl.Parse([]string{"--veyron.namespace.root=ab", "--veyron.namespace.root=xy", "--veyron.namespace.root=ab"}, nil); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	rf := fl.RuntimeFlags()
	if got, want := len(rf.NamespaceRoots), 2; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
	if got, want := rf.NamespaceRoots, []string{"ab", "xy"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestConfig(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var testFlag1, testFlag2 string
	fs.StringVar(&testFlag1, "test_flag1", "default1", "")
	fs.StringVar(&testFlag2, "test_flag2", "default2", "")
	fl := flags.CreateAndRegister(fs, flags.Runtime)
	args := []string{
		"--veyron.namespace.root=argRoot1",
		"--veyron.namespace.root=argRoot2",
		"--veyron.vtrace.cache_size=1234",
	}
	config := map[string]string{
		"veyron.namespace.root":    "configRoot",
		"veyron.credentials":       "configCreds",
		"veyron.vtrace.cache_size": "4321",
		"test_flag1":               "test value",
		"flag.that.does.not.exist": "some value",
	}
	if err := fl.Parse(args, config); err != nil {
		t.Errorf("Parse(%v, %v) failed: %v", args, config, err)
	}
	rtf := fl.RuntimeFlags()
	if got, want := rtf.NamespaceRoots, []string{"argRoot1", "argRoot2", "configRoot"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Namespace roots: got %v, want %v", got, want)
	}
	if got, want := rtf.Credentials, "configCreds"; got != want {
		t.Errorf("Credentials: got %v, want %v", got, want)
	}
	if got, want := testFlag1, "test value"; got != want {
		t.Errorf("Test flag 1: got %v, want %v", got, want)
	}
	if got, want := testFlag2, "default2"; got != want {
		t.Errorf("Test flag 2: got %v, want %v", got, want)
	}
	if got, want := rtf.Vtrace.CacheSize, 4321; got != want {
		t.Errorf("Test flag 2: got %v, want %v", got, want)
	}
}

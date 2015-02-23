package main_test

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"v.io/core/veyron/lib/testutil/v23tests"
)

//go:generate v23 test generate

// HACK: This is a hack to force v23 test generate to generate modules.Dispatch in TestMain.
// TODO(suharshs,cnicolaou): Find a way to get rid of this dummy subprocesses.
func dummy(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return nil
}

// redirect redirects the stdout of the given invocation to the file at the
// given path.
func redirect(t *v23tests.T, inv *v23tests.Invocation, path string) {
	if err := ioutil.WriteFile(path, []byte(inv.Output()), 0600); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v\n", path, err)
	}
}

// removePublicKeys replaces public keys (16 hex bytes, :-separated) with
// XX:....  This substitution enables comparison with golden output even when
// keys are freshly minted by the "principal create" command.
func removePublicKeys(input string) string {
	return regexp.MustCompile("([0-9a-f]{2}:){15}[0-9a-f]{2}").ReplaceAllString(input, "XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX")
}

func removeCaveats(input string) string {
	input = regexp.MustCompile(`0x54a676398137187ecdb26d2d69ba0004\(int64=.*\)`).ReplaceAllString(input, "ExpiryCaveat")
	input = regexp.MustCompile(`0x54a676398137187ecdb26d2d69ba0003\(\[]string=.*\)`).ReplaceAllString(input, "MethodCaveat")
	input = regexp.MustCompile(`0x00000000000000000000000000000000\(bool=true\)`).ReplaceAllString(input, "Unconstrained")
	return input
}

func V23TestBlessSelf(t *v23tests.T) {
	var (
		outputDir         = t.NewTempDir()
		aliceDir          = filepath.Join(outputDir, "alice")
		aliceBlessingFile = filepath.Join(outputDir, "aliceself")
	)

	bin := t.BuildGoPkg("v.io/core/veyron/tools/principal")
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	bin = bin.WithEnv("VEYRON_CREDENTIALS=" + aliceDir)
	redirect(t, bin.Start("blessself", "alicereborn"), aliceBlessingFile)
	got := removePublicKeys(bin.Start("dumpblessings", aliceBlessingFile).Output())
	want := `Blessings          : alicereborn
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 1
Chain #0 (1 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alicereborn with 0 caveats
`
	if want != got {
		t.Fatalf("unexpected output, wanted \n%s, got\n%s", want, got)
	}
}

func V23TestStore(t *v23tests.T) {
	var (
		outputDir   = t.NewTempDir()
		bin         = t.BuildGoPkg("v.io/core/veyron/tools/principal")
		aliceDir    = filepath.Join(outputDir, "alice")
		aliceFriend = filepath.Join(outputDir, "alice.bless")
		bobDir      = filepath.Join(outputDir, "bob")
		bobForPeer  = filepath.Join(outputDir, "bob.store.forpeer")
	)

	// Create two principals: alice and bob.
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
	bin.Start("create", bobDir, "bob").WaitOrDie(os.Stdout, os.Stderr)

	// Bless Alice with Bob's principal.
	blessEnv := []string{"VEYRON_CREDENTIALS=" + aliceDir}
	redirect(t, bin.WithEnv(blessEnv...).Start("bless", "--for=1m", bobDir, "friend"), aliceFriend)

	// Run store forpeer on bob.
	bin.Start("--veyron.credentials="+bobDir, "store", "set", aliceFriend, "alice").WaitOrDie(os.Stdout, os.Stderr)
	redirect(t, bin.WithEnv(blessEnv...).Start("--veyron.credentials="+bobDir, "store", "forpeer", "alice/server"), bobForPeer)

	got := removeCaveats(removePublicKeys(bin.Start("dumpblessings", bobForPeer).Output()))
	want := `Blessings          : bob#alice/friend
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 2
Chain #0 (1 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: bob with 0 caveats
Chain #1 (2 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alice with 0 caveats
  Certificate #1: friend with 1 caveat
    (0) ExpiryCaveat
`
	if want != got {
		t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
	}
}

func V23TestDump(t *v23tests.T) {
	var (
		outputDir = t.NewTempDir()
		bin       = t.BuildGoPkg("v.io/core/veyron/tools/principal")
		aliceDir  = filepath.Join(outputDir, "alice")
	)

	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	blessEnv := []string{"VEYRON_CREDENTIALS=" + aliceDir}
	got := removePublicKeys(bin.WithEnv(blessEnv...).Start("dump").Output())
	want := `Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default blessings: alice
Peer pattern                   : Blessings
...                            : alice
---------------- BlessingRoots ----------------
Public key                                      : Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [alice]
`
	if want != got {
		t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
	}
}

// Given an invocation of "principal recvblessings", this function returns the
// arguments to provide to "principal bless" provided by the "recvblessings"
// invocation.
//
// For example,
// principal recvblessings
// would typically print something like:
//    principal bless --remote_key=<some_public_key> --remote_token=<some_token> extensionfoo
// as an example of command line to use to send the blessings over.
//
// In that case, this method would return:
// { "--remote_key=<some_public_key>", "--remote_token=<some_token>", "extensionfoo"}
func blessArgsFromRecvBlessings(inv *v23tests.Invocation) []string {
	cmd := inv.ExpectSetEventuallyRE("(^principal bless .*$)")[0][0]
	return strings.Split(cmd, " ")[2:]
}

func V23TestRecvBlessings(t *v23tests.T) {
	var (
		outputDir = t.NewTempDir()
		bin       = t.BuildGoPkg("v.io/core/veyron/tools/principal")
		aliceDir  = filepath.Join(outputDir, "alice")
		carolDir  = filepath.Join(outputDir, "carol")
	)

	// Generate principals for alice and carol.
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
	bin.Start("create", carolDir, "carol").WaitOrDie(os.Stdout, os.Stderr)

	// Run recvblessings on carol, and have alice send blessings over
	// (blessings received must be set as default and shareable with all peers).
	var args []string
	{
		inv := bin.Start("--veyron.credentials="+carolDir, "--veyron.tcp.address=127.0.0.1:0", "recvblessings")
		defer inv.Kill(syscall.SIGTERM)
		args = append([]string{"bless", "--require_caveats=false"}, blessArgsFromRecvBlessings(inv)...)
		// Replace the random extension suggested by recvblessings with "friend/carol"
		args[len(args)-1] = "friend/carol"
	}

	bin.WithEnv("VEYRON_CREDENTIALS="+aliceDir).Start(args...).WaitOrDie(os.Stdout, os.Stderr)

	// Run recvblessings on carol, and have alice send blessings over
	// (blessings received must be set as shareable with peers matching 'alice/...'.)
	{
		inv := bin.Start("--veyron.credentials="+carolDir, "--veyron.tcp.address=127.0.0.1:0", "recvblessings", "--for_peer=alice", "--set_default=false")
		defer inv.Kill(syscall.SIGTERM)
		// recvblessings suggests a random extension, find the extension and replace it with friend/carol/foralice.
		args = append([]string{"bless", "--require_caveats=false"}, blessArgsFromRecvBlessings(inv)...)
		args[len(args)-1] = "friend/carol/foralice"
	}
	bin.WithEnv("VEYRON_CREDENTIALS="+aliceDir).Start(args...).WaitOrDie(os.Stdout, os.Stderr)

	listenerInv := bin.Start("--veyron.credentials="+carolDir, "--veyron.tcp.address=127.0.0.1:0", "recvblessings", "--for_peer=alice/...", "--set_default=false", "--vmodule=*=2", "--logtostderr")
	defer listenerInv.Kill(syscall.SIGTERM)

	args = append([]string{"bless", "--require_caveats=false"}, blessArgsFromRecvBlessings(listenerInv)...)

	{
		// Mucking around with remote_key should fail.
		cpy := strings.Split(regexp.MustCompile("remote_key=").ReplaceAllString(strings.Join(args, " "), "remote_key=BAD"), " ")
		var buf bytes.Buffer
		if bin.WithEnv("VEYRON_CREDENTIALS="+aliceDir).Start(cpy...).Wait(os.Stdout, &buf) == nil {
			t.Fatalf("%v should have failed, but did not", cpy)
		}

		if want, got := "key mismatch", buf.String(); !strings.Contains(got, want) {
			t.Fatalf("expected %q to be contained within\n%s\n, but was not", want, got)
		}
	}

	{
		var buf bytes.Buffer
		// Mucking around with the token should fail.
		cpy := strings.Split(regexp.MustCompile("remote_token=").ReplaceAllString(strings.Join(args, " "), "remote_token=BAD"), " ")
		if bin.WithEnv("VEYRON_CREDENTIALS="+aliceDir).Start(cpy...).Wait(os.Stdout, &buf) == nil {
			t.Fatalf("%v should have failed, but did not", cpy)
		}

		if want, got := "blessings received from unexpected sender", buf.String(); !strings.Contains(got, want) {
			t.Fatalf("expected %q to be contained within\n%s\n, but was not", want, got)
		}
	}

	// Dump carol out, the only blessing that survives should be from the
	// first "bless" command. (alice/friend/carol).
	got := removePublicKeys(bin.Start("--veyron.credentials="+carolDir, "dump").Output())
	want := `Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default blessings: alice/friend/carol
Peer pattern                   : Blessings
...                            : alice/friend/carol
alice                          : alice/friend/carol/foralice
---------------- BlessingRoots ----------------
Public key                                      : Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [alice]
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [carol]
`
	if want != got {
		t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
	}
}

func V23TestFork(t *v23tests.T) {
	var (
		outputDir             = t.NewTempDir()
		bin                   = t.BuildGoPkg("v.io/core/veyron/tools/principal")
		aliceDir              = filepath.Join(outputDir, "alice")
		alicePhoneDir         = filepath.Join(outputDir, "alice-phone")
		alicePhoneCalendarDir = filepath.Join(outputDir, "alice-phone-calendar")
		tmpfile               = filepath.Join(outputDir, "tmpfile")
	)

	// Generate principals for alice.
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	// Run fork to setup up credentials for alice/phone that are
	// blessed by alice under the extension "phone".
	bin.Start("--veyron.credentials="+aliceDir, "fork", "--for", "1h", alicePhoneDir, "phone").WaitOrDie(os.Stdout, os.Stderr)

	// Dump alice-phone out, the only blessings it has must be from alice (alice/phone).
	{
		got := removePublicKeys(bin.Start("--veyron.credentials="+alicePhoneDir, "dump").Output())
		want := `Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default blessings: alice/phone
Peer pattern                   : Blessings
...                            : alice/phone
---------------- BlessingRoots ----------------
Public key                                      : Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [alice]
`
		if want != got {
			t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
		}
	}
	// And it should have an expiry caveat
	{
		redirect(t, bin.Start("--veyron.credentials", alicePhoneDir, "store", "default"), tmpfile)
		got := removeCaveats(removePublicKeys(bin.Start("dumpblessings", tmpfile).Output()))
		want := `Blessings          : alice/phone
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 1
Chain #0 (2 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alice with 0 caveats
  Certificate #1: phone with 1 caveat
    (0) ExpiryCaveat
`
		if want != got {
			t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
		}
	}

	// Run fork to setup up credentials for alice/phone/calendar that are
	// blessed by alice/phone under the extension "calendar".
	bin.Start("--veyron.credentials="+alicePhoneDir, "fork", "--for", "1h", alicePhoneCalendarDir, "calendar").WaitOrDie(os.Stdout, os.Stderr)
	{
		got := removePublicKeys(bin.Start("--veyron.credentials="+alicePhoneCalendarDir, "dump").Output())
		want := `Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default blessings: alice/phone/calendar
Peer pattern                   : Blessings
...                            : alice/phone/calendar
---------------- BlessingRoots ----------------
Public key                                      : Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX : [alice]
`
		if want != got {
			t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
		}
	}
	{
		redirect(t, bin.Start("--veyron.credentials", alicePhoneCalendarDir, "store", "default"), tmpfile)
		got := removeCaveats(removePublicKeys(bin.Start("dumpblessings", tmpfile).Output()))
		want := `Blessings          : alice/phone/calendar
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 1
Chain #0 (3 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alice with 0 caveats
  Certificate #1: phone with 1 caveat
    (0) ExpiryCaveat
  Certificate #2: calendar with 1 caveat
    (0) ExpiryCaveat
`
		if want != got {
			t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
		}
	}
}

func V23TestCreate(t *v23tests.T) {
	var (
		outputDir = t.NewTempDir()
		bin       = t.BuildGoPkg("v.io/core/veyron/tools/principal")
		aliceDir  = filepath.Join(outputDir, "alice")
	)

	// Creating a principal should succeed the first time.
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	// The second time should fail (the create command won't override an existing principal).
	if bin.Start("create", aliceDir, "alice").Wait(os.Stdout, os.Stderr) == nil {
		t.Fatalf("principal creation should have failed, but did not")
	}

	// If we specify -overwrite, it will.
	bin.Start("create", "--overwrite", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
}

func V23TestCaveats(t *v23tests.T) {
	var (
		outputDir         = t.NewTempDir()
		aliceDir          = filepath.Join(outputDir, "alice")
		aliceBlessingFile = filepath.Join(outputDir, "aliceself")
	)

	bin := t.BuildGoPkg("v.io/core/veyron/tools/principal")
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	bin = bin.WithEnv("VEYRON_CREDENTIALS=" + aliceDir)
	args := []string{
		"blessself",
		"--caveat=\"v.io/core/veyron2/security\".MethodCaveatX={\"method\"}",
		"--caveat={{0x54,0xa6,0x76,0x39,0x81,0x37,0x18,0x7e,0xcd,0xb2,0x6d,0x2d,0x69,0xba,0x0,0x3},typeobject([]string)}={\"method\"}",
		"alicereborn",
	}
	redirect(t, bin.Start(args...), aliceBlessingFile)
	got := removeCaveats(removePublicKeys(bin.Start("dumpblessings", aliceBlessingFile).Output()))
	want := `Blessings          : alicereborn
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 1
Chain #0 (1 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alicereborn with 2 caveats
    (0) MethodCaveat
    (1) MethodCaveat
`
	if want != got {
		t.Fatalf("unexpected output, wanted \n%s, got\n%s", want, got)
	}
}

func V23TestForkWithoutVDLPATH(t *v23tests.T) {
	var (
		parent = t.NewTempDir()
		bin    = t.BuildGoPkg("v.io/core/veyron/tools/principal").WithEnv("VANADIUM_ROOT=''", "VDLPATH=''")
	)
	if err := bin.Start("create", parent, "parent").Wait(os.Stdout, os.Stderr); err != nil {
		t.Fatalf("create %q failed: %v", parent, err)
	}
	if err := bin.Start("--veyron.credentials="+parent, "fork", "--for=1s", t.NewTempDir(), "child").Wait(os.Stdout, os.Stderr); err != nil {
		t.Errorf("fork failed: %v", err)
	}
}

func V23TestForkWithoutCaveats(t *v23tests.T) {
	var (
		parent = t.NewTempDir()
		child  = t.NewTempDir()
		bin    = t.BuildGoPkg("v.io/core/veyron/tools/principal")
		buf    bytes.Buffer
	)
	if err := bin.Start("create", parent, "parent").Wait(os.Stdout, os.Stderr); err != nil {
		t.Fatalf("create %q failed: %v", parent, err)
	}
	if err := bin.Start("--veyron.credentials", parent, "fork", child, "child").Wait(os.Stdout, &buf); err == nil {
		t.Errorf("fork should have failed without any caveats, but did not")
	} else if got, want := buf.String(), "ERROR: no caveats provided"; !strings.Contains(got, want) {
		t.Errorf("fork returned error: %q, expected error to contain %q", got, want)
	}
	if err := bin.Start("--veyron.credentials", parent, "fork", "--for=0", child, "child").Wait(os.Stdout, &buf); err == nil {
		t.Errorf("fork should have failed without any caveats, but did not")
	} else if got, want := buf.String(), "ERROR: no caveats provided"; !strings.Contains(got, want) {
		t.Errorf("fork returned error: %q, expected error to contain %q", got, want)
	}
	if err := bin.Start("--veyron.credentials", parent, "fork", "--require_caveats=false", child, "child").Wait(os.Stdout, os.Stderr); err != nil {
		t.Errorf("fork --require_caveats=false failed with: %v", err)
	}
}

func V23TestBless(t *v23tests.T) {
	var (
		bin      = t.BuildGoPkg("v.io/core/veyron/tools/principal")
		dir      = t.NewTempDir()
		aliceDir = filepath.Join(dir, "alice")
		bobDir   = filepath.Join(dir, "bob")
		tmpfile  = filepath.Join(dir, "tmpfile")
	)
	// Create two principals: alice and bob
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
	bin.Start("create", bobDir, "bob").WaitOrDie(os.Stdout, os.Stderr)

	// All blessings will be done by "alice"
	bin = bin.WithEnv("VEYRON_CREDENTIALS=" + aliceDir)

	{
		// "alice" should fail to bless "bob" without any caveats
		var buf bytes.Buffer
		if err := bin.Start("bless", bobDir, "friend").Wait(os.Stdout, &buf); err == nil {
			t.Errorf("bless should have failed when no caveats are specified")
		} else if got, want := buf.String(), "ERROR: no caveats provided"; !strings.Contains(got, want) {
			t.Errorf("got error %q, expected to match %q", got, want)
		}
	}
	{
		// But succeed if --require_caveats=false is specified
		redirect(t, bin.Start("bless", "--require_caveats=false", bobDir, "friend"), tmpfile)
		got := removeCaveats(removePublicKeys(bin.Start("dumpblessings", tmpfile).Output()))
		want := `Blessings          : alice/friend
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 1
Chain #0 (2 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alice with 0 caveats
  Certificate #1: friend with 1 caveat
    (0) Unconstrained
`
		if got != want {
			t.Errorf("Got\n%vWant\n%v", got, want)
		}
	}
	{
		// And succeed if --for is specified
		redirect(t, bin.Start("bless", "--for=1m", bobDir, "friend"), tmpfile)
		got := removeCaveats(removePublicKeys(bin.Start("dumpblessings", tmpfile).Output()))
		want := `Blessings          : alice/friend
PublicKey          : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
Certificate chains : 1
Chain #0 (2 certificates). Root certificate public key: XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
  Certificate #0: alice with 0 caveats
  Certificate #1: friend with 1 caveat
    (0) ExpiryCaveat
`
		if got != want {
			t.Errorf("Got\n%vWant\n%v", got, want)
		}
	}
	{
		// But not if --for=0
		var buf bytes.Buffer
		if err := bin.Start("bless", "--for=0", bobDir, "friend").Wait(os.Stdout, &buf); err == nil {
			t.Errorf("bless should have failed when no caveats are specified")
		} else if got, want := buf.String(), "ERROR: no caveats provided"; !strings.Contains(got, want) {
			t.Errorf("got error %q, expected to match %q", got, want)
		}
	}
}

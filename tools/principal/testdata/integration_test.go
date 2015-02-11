package testdata

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil/v23tests"
	_ "v.io/core/veyron/profiles"
)

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

// redirect redirects the stdout of the given invocation to the file at the
// given path.
func redirect(t *testing.T, inv v23tests.Invocation, path string) {
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
	return regexp.MustCompile(`0x54a676398137187ecdb26d2d69ba0004\(int64=.*\)`).ReplaceAllString(input, "ExpiryCaveat")
}

func TestBlessSelf(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	var (
		outputDir         = env.TempDir()
		aliceDir          = filepath.Join(outputDir, "alice")
		aliceBlessingFile = filepath.Join(outputDir, "aliceself")
	)

	bin := env.BuildGoPkg("v.io/core/veyron/tools/principal")
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

func TestStore(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	var (
		outputDir   = env.TempDir()
		bin         = env.BuildGoPkg("v.io/core/veyron/tools/principal")
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
	redirect(t, bin.WithEnv(blessEnv...).Start("bless", bobDir, "friend"), aliceFriend)

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
		t.Fatalf("unexpected output, wanted\n%s, got\n%s", want, got)
	}
}

func TestDump(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	var (
		outputDir = env.TempDir()
		bin       = env.BuildGoPkg("v.io/core/veyron/tools/principal")
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
		t.Fatalf("unexpected output, wanted\n%s, got\n%s", want, got)
	}
}

func TestRecvBlessings(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	outputDir := env.TempDir()
	bin := env.BuildGoPkg("v.io/core/veyron/tools/principal")

	var (
		aliceDir = filepath.Join(outputDir, "alice")
		carolDir = filepath.Join(outputDir, "carol")
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
		e := expect.NewSession(t, inv.Stdout(), 3*time.Second)
		// recvblessings suggests a random extension, find the extension and replace it with friend/carol.
		cmd := e.ExpectSetEventuallyRE("(^principal bless .*$)")[0][0]
		args = strings.Split(regexp.MustCompile("extension[0-9]*").ReplaceAllString(cmd, "friend/carol"), " ")
	}

	bin.WithEnv("VEYRON_CREDENTIALS="+aliceDir).Start(args[1:]...).WaitOrDie(os.Stdout, os.Stderr)

	// Run recvblessings on carol, and have alice send blessings over
	// (blessings received must be set as shareable with peers matching 'alice/...'.)
	{
		inv := bin.Start("--veyron.credentials="+carolDir, "--veyron.tcp.address=127.0.0.1:0", "recvblessings", "--for_peer=alice", "--set_default=false")
		defer inv.Kill(syscall.SIGTERM)
		e := expect.NewSession(t, inv.Stdout(), 3*time.Second)
		// recvblessings suggests a random extension, find the extension and replace it with friend/carol/foralice.
		cmd := e.ExpectSetEventuallyRE("(^principal bless .*$)")[0][0]
		args = strings.Split(regexp.MustCompile("extension[0-9]*").ReplaceAllString(cmd, "friend/carol/foralice"), " ")
	}
	bin.WithEnv("VEYRON_CREDENTIALS="+aliceDir).Start(args[1:]...).WaitOrDie(os.Stdout, os.Stderr)

	listenerInv := bin.Start("--veyron.credentials="+carolDir, "--veyron.tcp.address=127.0.0.1:0", "recvblessings", "--for_peer=alice/...", "--set_default=false", "--vmodule=*=2", "--logtostderr")
	defer listenerInv.Kill(syscall.SIGTERM)

	e := expect.NewSession(t, listenerInv.Stdout(), 3*time.Second)
	sendCmd := e.ExpectSetEventuallyRE("(^principal bless .*$)")[0][0]
	{
		// Mucking around with the key should fail.
		args = strings.Split(regexp.MustCompile("remote_key=").ReplaceAllString(sendCmd, "remote_key=BAD"), " ")
	}

	{
		var buf bytes.Buffer
		if bin.WithEnv("VEYRON_CREDENTIALS="+aliceDir).Start(args[1:]...).Wait(os.Stdout, &buf) == nil {
			t.Fatalf("sender should have failed but did not")
		}

		if want, got := "key mismatch", buf.String(); !strings.Contains(got, want) {
			t.Fatalf("expected %q to be contained within\n%s\n, but was not", want, got)
		}
	}

	{
		var buf bytes.Buffer
		// Mucking around with the token should fail.
		args = strings.Split(regexp.MustCompile("remote_token=").ReplaceAllString(sendCmd, "remote_token=BAD"), " ")
		if bin.WithEnv("VEYRON_CREDENTIALS="+aliceDir).Start(args[1:]...).Wait(os.Stdout, &buf) == nil {
			t.Fatalf("sender should have failed but did not")
		}

		if want, got := "blessings received from unexpected sender", buf.String(); !strings.Contains(got, want) {
			t.Fatalf("expected %q to be contained within\n%s\n, but was not", want, got)
		}
	}

	// Dump carol out, the only blessing that survives should be from the
	// first "bless" command. (alice/friend/carol).
	{
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
			t.Fatalf("unexpected output, wanted\n%s, got\n%s", want, got)
		}
	}
}

func TestFork(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	var (
		outputDir             = env.TempDir()
		bin                   = env.BuildGoPkg("v.io/core/veyron/tools/principal")
		aliceDir              = filepath.Join(outputDir, "alice")
		alicePhoneDir         = filepath.Join(outputDir, "alice-phone")
		alicePhoneCalendarDir = filepath.Join(outputDir, "alice-phone-calendar")
	)

	// Generate principals for alice.
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	// Run fork to setup up credentials for alice/phone that are
	// blessed by alice under the extension "phone".
	bin.Start("--veyron.credentials="+aliceDir, "fork", alicePhoneDir, "phone").WaitOrDie(os.Stdout, os.Stderr)

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
			t.Fatalf("unexpected output, wanted\n%s, got\n%s", want, got)
		}
	}

	// Run fork to setup up credentials for alice/phone/calendar that are
	// blessed by alice-phone under the extension "calendar".
	bin.Start("--veyron.credentials="+alicePhoneDir, "fork", alicePhoneCalendarDir, "calendar").WaitOrDie(os.Stdout, os.Stderr)
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
			t.Fatalf("unexpected output, wanted\n%s, got\n%s", want, got)
		}
	}
}

func TestCreate(t *testing.T) {
	env := v23tests.New(t)
	defer env.Cleanup()

	var (
		outputDir = env.TempDir()
		bin       = env.BuildGoPkg("v.io/core/veyron/tools/principal")
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

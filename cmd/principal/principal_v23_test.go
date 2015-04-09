// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"v.io/x/ref/envvar"
	"v.io/x/ref/test/v23tests"
)

//go:generate v23 test generate

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
	input = regexp.MustCompile(`0xa64c2d0119fba3348071feeb2f308000\(time\.Time=.*\)`).ReplaceAllString(input, "ExpiryCaveat")
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

	bin := t.BuildGoPkg("v.io/x/ref/cmd/principal")
	bin.Run("create", aliceDir, "alice")

	bin = bin.WithEnv(credEnv(aliceDir))
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
		bin         = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		aliceDir    = filepath.Join(outputDir, "alice")
		aliceFriend = filepath.Join(outputDir, "alice.bless")
		bobDir      = filepath.Join(outputDir, "bob")
		bobForPeer  = filepath.Join(outputDir, "bob.store.forpeer")
	)

	// Create two principals: alice and bob.
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
	bin.Start("create", bobDir, "bob").WaitOrDie(os.Stdout, os.Stderr)

	// Bless Alice with Bob's principal.
	blessEnv := credEnv(aliceDir)
	redirect(t, bin.WithEnv(blessEnv).Start("bless", "--for=1m", bobDir, "friend"), aliceFriend)

	// Run store forpeer on bob.
	bin.Start("--v23.credentials="+bobDir, "set", "forpeer", aliceFriend, "alice").WaitOrDie(os.Stdout, os.Stderr)
	redirect(t, bin.WithEnv(blessEnv).Start("--v23.credentials="+bobDir, "get", "forpeer", "alice/server"), bobForPeer)

	got := removeCaveats(removePublicKeys(bin.Start("dumpblessings", bobForPeer).Output()))
	want := `Blessings          : bob,alice/friend
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
		bin       = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		aliceDir  = filepath.Join(outputDir, "alice")
	)

	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	blessEnv := credEnv(aliceDir)
	got := removePublicKeys(bin.WithEnv(blessEnv).Start("dump").Output())
	want := `Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default Blessings                alice
Peer pattern                     Blessings
...                              alice
---------------- BlessingRoots ----------------
Public key                                        Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX   [alice]
`
	if want != got {
		t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
	}
}

func V23TestGetRecognizedRoots(t *v23tests.T) {
	var (
		outputDir = t.NewTempDir()
		bin       = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		aliceDir  = filepath.Join(outputDir, "alice")
	)

	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	blessEnv := credEnv(aliceDir)
	got := removePublicKeys(bin.WithEnv(blessEnv).Start("get", "recognizedroots").Output())
	want := `Public key                                        Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX   [alice]
`
	if want != got {
		t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
	}
}

func V23TestGetPeermap(t *v23tests.T) {
	var (
		outputDir = t.NewTempDir()
		bin       = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		aliceDir  = filepath.Join(outputDir, "alice")
	)

	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	blessEnv := credEnv(aliceDir)
	got := removePublicKeys(bin.WithEnv(blessEnv).Start("get", "peermap").Output())
	want := `Default Blessings                alice
Peer pattern                     Blessings
...                              alice
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
//    principal bless --remote-key=<some_public_key> --remote-token=<some_token> extensionfoo
// as an example of command line to use to send the blessings over.
//
// In that case, this method would return:
// { "--remote-key=<some_public_key>", "--remote-token=<some_token>", "extensionfoo"}
func blessArgsFromRecvBlessings(inv *v23tests.Invocation) []string {
	cmd := inv.ExpectSetEventuallyRE("(^principal bless .*$)")[0][0]
	return strings.Split(cmd, " ")[2:]
}

func V23TestRecvBlessings(t *v23tests.T) {
	var (
		outputDir    = t.NewTempDir()
		bin          = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		aliceDir     = filepath.Join(outputDir, "alice")
		bobDir       = filepath.Join(outputDir, "bob")
		carolDir     = filepath.Join(outputDir, "carol")
		bobBlessFile = filepath.Join(outputDir, "bobBlessInfo")
	)

	// Generate principals for alice and carol.
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
	bin.Start("create", bobDir, "bob").WaitOrDie(os.Stdout, os.Stderr)
	bin.Start("create", carolDir, "carol").WaitOrDie(os.Stdout, os.Stderr)

	// Run recvblessings on carol, and have alice send blessings over
	// (blessings received must be set as default and shareable with all peers).
	var args []string
	{
		inv := bin.Start("--v23.credentials="+carolDir, "--v23.tcp.address=127.0.0.1:0", "recvblessings")
		args = append([]string{"bless", "--require-caveats=false"}, blessArgsFromRecvBlessings(inv)...)
		// Replace the random extension suggested by recvblessings with "friend/carol"
		args[len(args)-1] = "friend/carol"
	}
	bin.WithEnv(credEnv(aliceDir)).Start(args...).WaitOrDie(os.Stdout, os.Stderr)

	// Run recvblessings on carol, and have alice send blessings over
	// (blessings received must be set as shareable with peers matching 'alice/...'.)
	{
		inv := bin.Start("--v23.credentials="+carolDir, "--v23.tcp.address=127.0.0.1:0", "recvblessings", "--for-peer=alice", "--set-default=false")
		// recvblessings suggests a random extension, find the extension and replace it with friend/carol/foralice.
		args = append([]string{"bless", "--require-caveats=false"}, blessArgsFromRecvBlessings(inv)...)
		args[len(args)-1] = "friend/carol/foralice"
	}
	bin.WithEnv(credEnv(aliceDir)).Start(args...).WaitOrDie(os.Stdout, os.Stderr)

	// Run recvblessings on carol with the --remote-arg-file flag, and have bob send blessings over with the --remote-arg-file flag.
	{
		inv := bin.Start("--v23.credentials="+carolDir, "--v23.tcp.address=127.0.0.1:0", "recvblessings", "--for-peer=bob", "--set-default=false", "--remote-arg-file="+bobBlessFile)
		// recvblessings suggests a random extension, use friend/carol/forbob instead.
		args = append([]string{"bless", "--require-caveats=false"}, blessArgsFromRecvBlessings(inv)...)
		args[len(args)-1] = "friend/carol/forbob"
	}
	bin.WithEnv(credEnv(bobDir)).Start(args...).WaitOrDie(os.Stdout, os.Stderr)

	listenerInv := bin.Start("--v23.credentials="+carolDir, "--v23.tcp.address=127.0.0.1:0", "recvblessings", "--for-peer=alice/...", "--set-default=false", "--vmodule=*=2", "--logtostderr")

	args = append([]string{"bless", "--require-caveats=false"}, blessArgsFromRecvBlessings(listenerInv)...)

	{
		// Mucking around with remote-key should fail.
		cpy := strings.Split(regexp.MustCompile("remote-key=").ReplaceAllString(strings.Join(args, " "), "remote-key=BAD"), " ")
		var buf bytes.Buffer
		if bin.WithEnv(credEnv(aliceDir)).Start(cpy...).Wait(os.Stdout, &buf) == nil {
			t.Fatalf("%v should have failed, but did not", cpy)
		}

		if want, got := "key mismatch", buf.String(); !strings.Contains(got, want) {
			t.Fatalf("expected %q to be contained within\n%s\n, but was not", want, got)
		}
	}

	{
		var buf bytes.Buffer
		// Mucking around with the token should fail.
		cpy := strings.Split(regexp.MustCompile("remote-token=").ReplaceAllString(strings.Join(args, " "), "remote-token=BAD"), " ")
		if bin.WithEnv(credEnv(aliceDir)).Start(cpy...).Wait(os.Stdout, &buf) == nil {
			t.Fatalf("%v should have failed, but did not", cpy)
		}

		if want, got := "blessings received from unexpected sender", buf.String(); !strings.Contains(got, want) {
			t.Fatalf("expected %q to be contained within\n%s\n, but was not", want, got)
		}
	}

	// Dump carol out, the only blessing that survives should be from the
	// first "bless" command. (alice/friend/carol).
	got := removePublicKeys(bin.Start("--v23.credentials="+carolDir, "dump").Output())
	want := `Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default Blessings                alice/friend/carol
Peer pattern                     Blessings
...                              alice/friend/carol
alice                            alice/friend/carol/foralice
bob                              bob/friend/carol/forbob
---------------- BlessingRoots ----------------
Public key                                        Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX   [alice]
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX   [bob]
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX   [carol]
`
	if want != got {
		t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
	}
}

func V23TestFork(t *v23tests.T) {
	var (
		outputDir             = t.NewTempDir()
		bin                   = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		aliceDir              = filepath.Join(outputDir, "alice")
		alicePhoneDir         = filepath.Join(outputDir, "alice-phone")
		alicePhoneCalendarDir = filepath.Join(outputDir, "alice-phone-calendar")
		tmpfile               = filepath.Join(outputDir, "tmpfile")
	)

	// Generate principals for alice.
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	// Run fork to setup up credentials for alice/phone that are
	// blessed by alice under the extension "phone".
	bin.Start("--v23.credentials="+aliceDir, "fork", "--for", "1h", alicePhoneDir, "phone").WaitOrDie(os.Stdout, os.Stderr)

	// Dump alice-phone out, the only blessings it has must be from alice (alice/phone).
	{
		got := removePublicKeys(bin.Start("--v23.credentials="+alicePhoneDir, "dump").Output())
		want := `Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default Blessings                alice/phone
Peer pattern                     Blessings
...                              alice/phone
---------------- BlessingRoots ----------------
Public key                                        Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX   [alice]
`
		if want != got {
			t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
		}
	}
	// And it should have an expiry caveat
	{
		redirect(t, bin.Start("--v23.credentials", alicePhoneDir, "get", "default"), tmpfile)
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
	bin.Start("--v23.credentials="+alicePhoneDir, "fork", "--for", "1h", alicePhoneCalendarDir, "calendar").WaitOrDie(os.Stdout, os.Stderr)
	{
		got := removePublicKeys(bin.Start("--v23.credentials="+alicePhoneCalendarDir, "dump").Output())
		want := `Public key : XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX
---------------- BlessingStore ----------------
Default Blessings                alice/phone/calendar
Peer pattern                     Blessings
...                              alice/phone/calendar
---------------- BlessingRoots ----------------
Public key                                        Pattern
XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX:XX   [alice]
`
		if want != got {
			t.Fatalf("unexpected output, got\n%s, wanted\n%s", got, want)
		}
	}
	{
		redirect(t, bin.Start("--v23.credentials", alicePhoneCalendarDir, "get", "default"), tmpfile)
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
		bin       = t.BuildGoPkg("v.io/x/ref/cmd/principal")
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

	bin := t.BuildGoPkg("v.io/x/ref/cmd/principal")
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)

	bin = bin.WithEnv(credEnv(aliceDir))
	args := []string{
		"blessself",
		"--caveat=\"v.io/v23/security\".MethodCaveatX={\"method\"}",
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
		bin    = t.BuildGoPkg("v.io/x/ref/cmd/principal").WithEnv("V23_ROOT=''", "VDLPATH=''")
	)
	if err := bin.Start("create", parent, "parent").Wait(os.Stdout, os.Stderr); err != nil {
		t.Fatalf("create %q failed: %v", parent, err)
	}
	if err := bin.Start("--v23.credentials="+parent, "fork", "--for=1s", t.NewTempDir(), "child").Wait(os.Stdout, os.Stderr); err != nil {
		t.Errorf("fork failed: %v", err)
	}
}

func V23TestForkWithoutCaveats(t *v23tests.T) {
	var (
		parent = t.NewTempDir()
		child  = t.NewTempDir()
		bin    = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		buf    bytes.Buffer
	)
	if err := bin.Start("create", parent, "parent").Wait(os.Stdout, os.Stderr); err != nil {
		t.Fatalf("create %q failed: %v", parent, err)
	}
	if err := bin.Start("--v23.credentials", parent, "fork", child, "child").Wait(os.Stdout, &buf); err == nil {
		t.Errorf("fork should have failed without any caveats, but did not")
	} else if got, want := buf.String(), "ERROR: no caveats provided"; !strings.Contains(got, want) {
		t.Errorf("fork returned error: %q, expected error to contain %q", got, want)
	}
	if err := bin.Start("--v23.credentials", parent, "fork", "--for=0", child, "child").Wait(os.Stdout, &buf); err == nil {
		t.Errorf("fork should have failed without any caveats, but did not")
	} else if got, want := buf.String(), "ERROR: no caveats provided"; !strings.Contains(got, want) {
		t.Errorf("fork returned error: %q, expected error to contain %q", got, want)
	}
	if err := bin.Start("--v23.credentials", parent, "fork", "--require-caveats=false", child, "child").Wait(os.Stdout, os.Stderr); err != nil {
		t.Errorf("fork --require-caveats=false failed with: %v", err)
	}
}

func V23TestBless(t *v23tests.T) {
	var (
		bin      = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		dir      = t.NewTempDir()
		aliceDir = filepath.Join(dir, "alice")
		bobDir   = filepath.Join(dir, "bob")
		tmpfile  = filepath.Join(dir, "tmpfile")
	)
	// Create two principals: alice and bob
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
	bin.Start("create", bobDir, "bob").WaitOrDie(os.Stdout, os.Stderr)

	// All blessings will be done by "alice"
	bin = bin.WithEnv(credEnv(aliceDir))

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
		// But succeed if --require-caveats=false is specified
		redirect(t, bin.Start("bless", "--require-caveats=false", bobDir, "friend"), tmpfile)
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

func V23TestAddBlessingsToRoots(t *v23tests.T) {
	var (
		bin          = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		aliceDir     = t.NewTempDir()
		bobDir       = t.NewTempDir()
		blessingFile = filepath.Join(t.NewTempDir(), "bobfile")

		// Extract the public key from the first line of output from
		// "principal dump", which is formatted as:
		// Public key : <the public key>
		publicKey = func(dir string) string {
			output := bin.Start("--v23.credentials="+dir, "dump").Output()
			line := strings.SplitN(output, "\n", 2)[0]
			fields := strings.Split(line, " ")
			return fields[len(fields)-1]
		}
	)
	// Create two principals, "alice" and "bob"
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
	bin.Start("create", bobDir, "bob").WaitOrDie(os.Stdout, os.Stderr)
	// Have bob create a "bob/friend" blessing and have alice recognize that.
	redirect(t, bin.Start("--v23.credentials="+bobDir, "bless", "--require-caveats=false", aliceDir, "friend"), blessingFile)
	bin.Start("--v23.credentials="+aliceDir, "addtoroots", blessingFile).WaitOrDie(os.Stdout, os.Stderr)

	want := fmt.Sprintf(`Public key                                        Pattern
%v   [alice]
%v   [bob]
`, publicKey(aliceDir), publicKey(bobDir))

	// Finally view alice's recognized roots, it should have lines corresponding to aliceLine and bobLine.
	got := bin.Start("--v23.credentials="+aliceDir, "get", "recognizedroots").Output()
	if got != want {
		t.Fatalf("Got:\n%v\n\nWant:\n%v", got, want)
	}
}

func V23TestAddKeyToRoots(t *v23tests.T) {
	var (
		bin      = t.BuildGoPkg("v.io/x/ref/cmd/principal")
		aliceDir = t.NewTempDir()
	)
	bin.Start("create", aliceDir, "alice").WaitOrDie(os.Stdout, os.Stderr)
	// The second argument and the "want" line below were generated by:
	//   import "encoding/base64"
	//   import "v.io/x/ref/lib/security"
	//
	//    key, _, _ := security.NewPrincipalKey()
	//    der, _ := key.MarshalBinary()
	//    b64 := base64.URLEncoding.EncodeToString(der)  // argument to addtoroots
	//    str := fmt.Sprintf("%v", key)                  // for the "want" line
	bin.Start("--v23.credentials="+aliceDir, "addtoroots", "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE9iRjaFDoGJI9tarUwWqIW31ti72krThkYByn1v9Lf89D9VA0Mg2oUL7FDDM7qxjZcVM1ktM_W4tBfMVuRZmVCA==", "some_other_provider").WaitOrDie(os.Stdout, os.Stderr)
	// "foo" should appear in the set of BlessingRoots
	output := bin.Start("--v23.credentials="+aliceDir, "dump").Output()
	want := fmt.Sprintf("41:26:f6:aa:54:f9:31:d4:9d:1f:d2:69:c6:c5:50:70   [some_other_provider]")
	for _, line := range strings.Split(output, "\n") {
		if line == want {
			return
		}
	}
	t.Errorf("Could not find line:\n%v\nin output:\n%v\n", want, output)
}

func credEnv(dir string) string {
	return fmt.Sprintf("%s=%s", envvar.Credentials, dir)
}

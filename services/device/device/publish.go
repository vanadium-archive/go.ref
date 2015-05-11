// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/application"
	"v.io/v23/services/permissions"
	"v.io/v23/verror"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/services/internal/binarylib"
	"v.io/x/ref/services/repository"
)

// TODO(caprita): Add unit test.

// TODO(caprita): Extend to include env, args, packages.

var cmdPublish = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runPublish),
	Name:   "publish",
	Short:  "Publish the given application(s).",
	Long: `
Publishes the given application(s) to the binary and application servers.
The <title> can be optionally specified with @<title> (defaults to the binary
name).
The binaries should be in $V23_ROOT/release/go/bin/[<GOOS>_<GOARCH>].
The binary is published as <binserv>/<binary name>/<GOOS>-<GOARCH>/<TIMESTAMP>.
The application envelope is published as <appserv>/<binary name>/0.
Optionally, adds blessing patterns to the Read and Resolve AccessLists.`,
	ArgsName: "<binary name>[@<title>] ...",
}

var binaryService, applicationService, readBlessings, goarchFlag, goosFlag string

func init() {
	cmdPublish.Flags.StringVar(&binaryService, "binserv", "binaries", "Name of binary service.")
	cmdPublish.Flags.StringVar(&applicationService, "appserv", "applications", "Name of application service.")
	cmdPublish.Flags.StringVar(&goosFlag, "goos", runtime.GOOS, "GOOS for application.  The default is the value of runtime.GOOS.")
	cmdPublish.Flags.Lookup("goos").DefValue = "<runtime.GOOS>"
	cmdPublish.Flags.StringVar(&goarchFlag, "goarch", runtime.GOARCH, "GOARCH for application.  The default is the value of runtime.GOARCH.")
	cmdPublish.Flags.Lookup("goarch").DefValue = "<runtime.GOARCH>"
	cmdPublish.Flags.StringVar(&readBlessings, "readers", "dev.v.io", "If non-empty, comma-separated blessing patterns to add to Read and Resolve AccessList.")
}

func setAccessLists(ctx *context.T, env *cmdline.Env, von string) error {
	if readBlessings == "" {
		return nil
	}
	perms, version, err := permissions.ObjectClient(von).GetPermissions(ctx)
	if err != nil {
		// TODO(caprita): This is a workaround until we sort out the
		// default AccessLists for applicationd (see issue #1317).  At that
		// time, uncomment the line below.
		//
		//   return err
		perms = make(access.Permissions)
	}
	for _, blessing := range strings.Split(readBlessings, ",") {
		for _, tag := range []access.Tag{access.Read, access.Resolve} {
			perms.Add(security.BlessingPattern(blessing), string(tag))
		}
	}
	if err := permissions.ObjectClient(von).SetPermissions(ctx, perms, version); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Added patterns %q to Read,Resolve AccessList for %q\n", readBlessings, von)
	return nil
}

func publishOne(ctx *context.T, env *cmdline.Env, binPath, binary string) error {
	binaryName, title := binary, binary
	if parts := strings.SplitN(binary, "@", 2); len(parts) == 2 {
		binaryName, title = parts[0], parts[1]
	}
	// Step 1, upload the binary to the binary service.

	// TODO(caprita): Instead of the current timestamp, use each binary's
	// BuildTimestamp from the buildinfo.
	timestamp := time.Now().UTC().Format(time.RFC3339)
	binaryVON := naming.Join(binaryService, binaryName, fmt.Sprintf("%s-%s", goosFlag, goarchFlag), timestamp)
	binaryFile := filepath.Join(binPath, binaryName)
	// TODO(caprita): Take signature of binary and put it in the envelope.
	if _, err := binarylib.UploadFromFile(ctx, binaryVON, binaryFile); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Binary %q uploaded from file %s\n", binaryVON, binaryFile)

	// Step 2, set the perms for the uploaded binary.

	if err := setAccessLists(ctx, env, binaryVON); err != nil {
		return err
	}

	// Step 3, download existing envelope (or create a new one), update, and
	// upload to application service.

	// TODO(caprita): use the profile detection machinery and/or let user
	// specify profiles by hand.
	profiles := []string{fmt.Sprintf("%s-%s", goosFlag, goarchFlag)}
	// TODO(caprita): use a label e.g. "prod" instead of "0".
	appVON := naming.Join(applicationService, binaryName, "0")
	appClient := repository.ApplicationClient(appVON)
	// NOTE: If profiles contains more than one entry, this will return only
	// the first match.  But presumably that's ok, since we're going to set
	// the envelopes for all the profiles to the same envelope anyway below.
	envelope, err := appClient.Match(ctx, profiles)
	if verror.ErrorID(err) == verror.ErrNoExist.ID {
		// There was nothing published yet, create a new envelope.
		envelope = application.Envelope{Title: title}
	} else if err != nil {
		return err
	}
	envelope.Binary.File = binaryVON
	if err := repository.ApplicationClient(appVON).Put(ctx, profiles, envelope); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Published %q\n", appVON)

	// Step 4, set the perms for the uploaded envelope.

	if err := setAccessLists(ctx, env, appVON); err != nil {
		return err
	}
	return nil
}

func runPublish(ctx *context.T, env *cmdline.Env, args []string) error {
	if expectedMin, got := 1, len(args); got < expectedMin {
		return env.UsageErrorf("publish: incorrect number of arguments, expected at least %d, got %d", expectedMin, got)
	}
	binaries := args
	vroot := os.Getenv("V23_ROOT")
	if vroot == "" {
		return env.UsageErrorf("publish: $V23_ROOT environment variable should be set")
	}
	binPath := filepath.Join(vroot, "release/go/bin")
	if goosFlag != runtime.GOOS || goarchFlag != runtime.GOARCH {
		binPath = filepath.Join(binPath, fmt.Sprintf("%s_%s", goosFlag, goarchFlag))
	}
	if fi, err := os.Stat(binPath); err != nil {
		return env.UsageErrorf("publish: failed to stat %v: %v", binPath, err)
	} else if !fi.IsDir() {
		return env.UsageErrorf("publish: %v is not a directory", binPath)
	}
	if binaryService == "" {
		return env.UsageErrorf("publish: --binserv must point to a binary service name")
	}
	if applicationService == "" {
		return env.UsageErrorf("publish: --appserv must point to an application service name")
	}
	var lastErr error
	for _, b := range binaries {
		if err := publishOne(ctx, env, binPath, b); err != nil {
			fmt.Fprintf(env.Stderr, "Failed to publish %q: %v\n", b, err)
			lastErr = err
		}
	}
	return lastErr
}

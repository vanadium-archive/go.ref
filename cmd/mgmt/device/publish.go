// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/services/security/access"
	"v.io/v23/services/security/access/object"
	"v.io/v23/verror"

	"v.io/x/lib/cmdline"
	appdimpl "v.io/x/ref/services/mgmt/application/impl"
	"v.io/x/ref/services/mgmt/lib/binary"
	irepos "v.io/x/ref/services/mgmt/repository"
)

// TODO(caprita): Add unit test.

// TODO(caprita): Extend to include env, args, packages.

var cmdPublish = &cmdline.Command{
	Run:   runPublish,
	Name:  "publish",
	Short: "Publish the given application(s).",
	Long: `
Publishes the given application(s) to the binary and application servers.
The binaries should be in $VANADIUM_ROOT/release/go/bin/[<GOOS>_<GOARCH>].
The binary is published as <binserv>/<binary name>/<GOOS>-<GOARCH>/<TIMESTAMP>.
The application envelope is published as <appserv>/<binary name>/0.
Optionally, adds blessing patterns to the Read and Resolve AccessLists.`,
	ArgsName: "<binary name> ...",
}

var binaryService, applicationService, readBlessings string

const (
	defaultArch = "$GOARCH"
	defaultOS   = "$GOOS"
)

var (
	goarchFlag flag.Getter
	goosFlag   flag.Getter
)

func init() {
	cmdPublish.Flags.StringVar(&binaryService, "binserv", "binaries", "Name of binary service.")
	cmdPublish.Flags.StringVar(&applicationService, "appserv", "applications", "Name of application service.")
	goosFlag = cmdline.RuntimeFlag("${GOOS}")
	goarchFlag = cmdline.RuntimeFlag("${GOARCH}")
	cmdPublish.Flags.Var(goosFlag, "goos", "GOOS for application.")
	cmdPublish.Flags.Var(goarchFlag, "goarch", "GOARCH for application.")
	cmdPublish.Flags.StringVar(&readBlessings, "readers", "dev.v.io", "If non-empty, comma-separated blessing patterns to add to Read and Resolve AccessList.")
}

func setAccessLists(cmd *cmdline.Command, von string) error {
	if readBlessings == "" {
		return nil
	}
	acl, etag, err := object.ObjectClient(von).GetPermissions(gctx)
	if err != nil {
		// TODO(caprita): This is a workaround until we sort out the
		// default AccessLists for applicationd (see issue #1317).  At that
		// time, uncomment the line below.
		//
		//   return err
		acl = make(access.Permissions)
	}
	for _, blessing := range strings.Split(readBlessings, ",") {
		for _, tag := range []access.Tag{access.Read, access.Resolve} {
			acl.Add(security.BlessingPattern(blessing), string(tag))
		}
	}
	if err := object.ObjectClient(von).SetPermissions(gctx, acl, etag); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Added patterns %q to Read,Resolve AccessList for %q\n", readBlessings, von)
	return nil
}

func publishOne(cmd *cmdline.Command, binPath, binaryName string) error {
	goos := goosFlag.Get().(string)
	goarch := goarchFlag.Get().(string)

	// Step 1, upload the binary to the binary service.

	// TODO(caprita): Instead of the current timestamp, use each binary's
	// BuildTimestamp from the buildinfo.
	timestamp := time.Now().UTC().Format(time.RFC3339)
	binaryVON := naming.Join(binaryService, binaryName, fmt.Sprintf("%s-%s", goos, goarch), timestamp)
	binaryFile := filepath.Join(binPath, binaryName)
	// TODO(caprita): Take signature of binary and put it in the envelope.
	if _, err := binary.UploadFromFile(gctx, binaryVON, binaryFile); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Binary %q uploaded from file %s\n", binaryVON, binaryFile)

	// Step 2, set the acls for the uploaded binary.

	if err := setAccessLists(cmd, binaryVON); err != nil {
		return err
	}

	// Step 3, download existing envelope (or create a new one), update, and
	// upload to application service.

	// TODO(caprita): use the profile detection machinery and/or let user
	// specify profiles by hand.
	profiles := []string{fmt.Sprintf("%s-%s", goos, goarch)}
	// TODO(caprita): use a label e.g. "prod" instead of "0".
	appVON := naming.Join(applicationService, binaryName, "0")
	appClient := irepos.ApplicationClient(appVON)
	// NOTE: If profiles contains more than one entry, this will return only
	// the first match.  But presumably that's ok, since we're going to set
	// the envelopes for all the profiles to the same envelope anyway below.
	envelope, err := appClient.Match(gctx, profiles)
	if verror.ErrorID(err) == appdimpl.ErrNotFound.ID {
		// There was nothing published yet, create a new envelope.
		envelope = application.Envelope{Title: binaryName}
	} else if err != nil {
		return err
	}
	envelope.Binary.File = binaryVON
	if err := irepos.ApplicationClient(appVON).Put(gctx, profiles, envelope); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Stdout(), "Published %q\n", appVON)

	// Step 4, set the acls for the uploaded envelope.

	if err := setAccessLists(cmd, appVON); err != nil {
		return err
	}
	return nil
}

func runPublish(cmd *cmdline.Command, args []string) error {
	if expectedMin, got := 1, len(args); got < expectedMin {
		return cmd.UsageErrorf("publish: incorrect number of arguments, expected at least %d, got %d", expectedMin, got)
	}
	binaries := args
	vroot := os.Getenv("VANADIUM_ROOT")
	if vroot == "" {
		return cmd.UsageErrorf("publish: $VANADIUM_ROOT environment variable should be set")
	}
	binPath := filepath.Join(vroot, "release/go/bin")
	goos := goosFlag.Get().(string)
	goarch := goarchFlag.Get().(string)
	if goos != runtime.GOOS || goarch != runtime.GOARCH {
		binPath = filepath.Join(binPath, fmt.Sprintf("%s_%s", goos, goarch))
	}
	if fi, err := os.Stat(binPath); err != nil {
		return cmd.UsageErrorf("publish: failed to stat %v: %v", binPath, err)
	} else if !fi.IsDir() {
		return cmd.UsageErrorf("publish: %v is not a directory", binPath)
	}
	if binaryService == "" {
		return cmd.UsageErrorf("publish: --binserv must point to a binary service name")
	}
	if applicationService == "" {
		return cmd.UsageErrorf("publish: --appserv must point to an application service name")
	}
	var lastErr error
	for _, b := range binaries {
		if err := publishOne(cmd, binPath, b); err != nil {
			fmt.Fprintf(cmd.Stderr(), "Failed to publish %q: %v\n", b, err)
			lastErr = err
		}
	}
	return lastErr
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $V23_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/device/internal/claim"
	"v.io/x/ref/services/identity"
)

var (
	permsDir     string
	blessingRoot string
)

func runServer(ctx *context.T, _ *cmdline.Env, _ []string) error {
	if blessingRoot != "" {
		addRoot(ctx, blessingRoot)
	}

	auth := securityflag.NewAuthorizerOrDie()
	claimable, claimed := claim.NewClaimableDispatcher(ctx, permsDir, "", auth)
	if claimable == nil {
		return errors.New("device is already claimed")
	}

	ctx, server, err := v23.WithNewDispatchingServer(ctx, "", claimable)
	if err != nil {
		return err
	}

	status := server.Status()
	ctx.Infof("Listening on: %v", status.Endpoints)
	if len(status.Endpoints) > 0 {
		fmt.Printf("NAME=%s\n", status.Endpoints[0].Name())
	}
	select {
	case <-claimed:
		return nil
	case s := <-signals.ShutdownOnSignals(ctx):
		return fmt.Errorf("received signal %v", s)
	}
}

func addRoot(ctx *context.T, jRoot string) {
	var bRoot identity.BlessingRootResponse
	if err := json.Unmarshal([]byte(jRoot), &bRoot); err != nil {
		ctx.Fatalf("unable to unmarshal the json blessing root: %v", err)
	}
	key, err := base64.URLEncoding.DecodeString(bRoot.PublicKey)
	if err != nil {
		ctx.Fatalf("unable to decode public key: %v", err)
	}
	roots := v23.GetPrincipal(ctx).Roots()
	for _, name := range bRoot.Names {
		if err := roots.Add(key, security.BlessingPattern(name)); err != nil {
			ctx.Fatalf("unable to add root: %v", err)
		}
	}
}

func main() {
	rootCmd := &cmdline.Command{
		Name:  "claimable",
		Short: "Run claimable server",
		Long: `
Claimable is a server that implements the Claimable interface from
v.io/v23/services/device. It exits immediately if the device is already
claimed. Otherwise, it keeps running until a successful Claim() request
is received.

It uses -v23.permissions.* to authorize the Claim request.
`,
		Runner: v23cmd.RunnerFunc(runServer),
	}
	rootCmd.Flags.StringVar(&permsDir, "perms-dir", "", "The directory where permissions will be stored.")
	rootCmd.Flags.StringVar(&blessingRoot, "blessing-root", "", "The blessing root to trust, JSON-encoded, e.g. from https://v.io/auth/blessing-root")
	cmdline.Main(rootCmd)
}

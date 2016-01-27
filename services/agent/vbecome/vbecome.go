// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/x/lib/cmdline"
	"v.io/x/ref"
	vsecurity "v.io/x/ref/lib/security"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/services/agent/internal/ipc"
	"v.io/x/ref/services/agent/internal/server"
	"v.io/x/ref/services/role"

	_ "v.io/x/ref/runtime/factories/generic"
)

var (
	durationFlag time.Duration
	timeoutFlag  time.Duration
	nameFlag     string
	roleFlag     string
)

var cmdVbecome = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(vbecome),
	Name:     "vbecome",
	Short:    "executes commands with a derived Vanadium principal",
	Long:     "Command vbecome executes commands with a derived Vanadium principal.",
	ArgsName: "<command> [command args...]",
}

const childAgentFd = 3
const keyServerFd = 4

func main() {
	cmdline.HideGlobalFlagsExcept()
	syscall.CloseOnExec(childAgentFd)
	syscall.CloseOnExec(keyServerFd)

	cmdVbecome.Flags.DurationVar(&durationFlag, "duration", 1*time.Hour, "Duration for the blessing.")
	cmdVbecome.Flags.DurationVar(&timeoutFlag, "timeout", 2*time.Minute, "Timeout for the RPCs.")
	cmdVbecome.Flags.StringVar(&nameFlag, "name", "", "Name to use for the blessing.")
	cmdVbecome.Flags.StringVar(&roleFlag, "role", "", "Role object from which to request the blessing. If set, the blessings from this role server are used and --name is ignored. If not set, the default blessings of the calling principal are extended with --name.")

	cmdline.Main(cmdVbecome)
}

func vbecome(ctx *context.T, env *cmdline.Env, args []string) error {
	if len(args) == 0 {
		if shell := env.Vars["SHELL"]; shell != "" {
			args = []string{shell}
		} else {
			return fmt.Errorf("You must specify a command to run.")
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	signer := security.NewInMemoryECDSASigner(key)
	principal, err := vsecurity.NewPrincipalFromSigner(signer, nil)
	if err != nil {
		return err
	}

	if len(roleFlag) == 0 {
		if len(nameFlag) == 0 {
			nameFlag = filepath.Base(args[0])
		}
		if err := bless(ctx, principal, nameFlag); err != nil {
			return err
		}
		ctx, err = v23.WithPrincipal(ctx, principal)
		if err != nil {
			return err
		}
	} else {
		// The role server expects the client's blessing name to end
		// with RoleSuffix. This is to avoid accidentally granting role
		// access to anything else that might have been blessed by the
		// same principal.
		if err := bless(ctx, principal, role.RoleSuffix); err != nil {
			return err
		}
		ctx, err = v23.WithPrincipal(ctx, principal)
		if err != nil {
			return err
		}
		if err = setupRoleBlessings(ctx, roleFlag); err != nil {
			return err
		}
	}

	// Clear out the environment variable before starting the child.
	if err = ref.EnvClearCredentials(); err != nil {
		return err
	}

	// Start an agent server.
	i := ipc.NewIPC()
	if err := server.ServeAgent(i, principal); err != nil {
		return err
	}
	dir, err := ioutil.TempDir("", "vbecome")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "sock")
	if err := i.Listen(path); err != nil {
		return err
	}
	defer i.Close()
	if err = os.Setenv(ref.EnvAgentPath, path); err != nil {
		ctx.Fatalf("setenv: %v", err)
	}

	return doExec(args)
}

func bless(ctx *context.T, p security.Principal, name string) error {
	caveat, err := security.NewExpiryCaveat(time.Now().Add(durationFlag))
	if err != nil {
		ctx.Errorf("Couldn't create caveat")
		return err
	}

	rp := v23.GetPrincipal(ctx)
	blessing, err := rp.Bless(p.PublicKey(), rp.BlessingStore().Default(), name, caveat)
	if err != nil {
		ctx.Errorf("Couldn't bless")
		return err
	}

	if err = p.BlessingStore().SetDefault(blessing); err != nil {
		ctx.Errorf("Couldn't set default blessing")
		return err
	}
	if _, err = p.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
		ctx.Errorf("Couldn't set default client blessing")
		return err
	}
	if err = security.AddToRoots(p, blessing); err != nil {
		ctx.Errorf("Couldn't set trusted roots")
		return err
	}
	return nil
}

func doExec(args []string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func setupRoleBlessings(ctx *context.T, roleStr string) error {
	ctx, cancel := context.WithTimeout(ctx, timeoutFlag)
	defer cancel()

	b, err := role.RoleClient(roleStr).SeekBlessings(ctx)
	if err != nil {
		return err
	}
	p := v23.GetPrincipal(ctx)
	// TODO(rthellend,ashankar): Revisit this configuration.
	// SetDefault: Should we expect users to want to act as a server on behalf of the role (by default?)
	// AllPrincipals: Do we not want to be discriminating about which services we use the role blessing at.
	if err := p.BlessingStore().SetDefault(b); err != nil {
		return err
	}
	if _, err := p.BlessingStore().Set(b, security.AllPrincipals); err != nil {
		return err
	}
	return nil
}

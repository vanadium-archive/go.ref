// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/flags/consts"
	"v.io/x/ref/security/agent"
	"v.io/x/ref/security/agent/keymgr"
	isecurity "v.io/x/ref/services/security"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/x/lib/vlog"

	_ "v.io/x/ref/profiles"
)

var (
	durationFlag time.Duration
	name         string
	role         string
)

var cmdVrun = &cmdline.Command{
	Run:      vrun,
	Name:     "vrun",
	Short:    "Executes a command with a derived principal.",
	Long:     "The vrun tool executes a command with a derived principal.",
	ArgsName: "<command> [command args...]",
}

func main() {
	syscall.CloseOnExec(3)
	syscall.CloseOnExec(4)

	cmdVrun.Flags.DurationVar(&durationFlag, "duration", 1*time.Hour, "Duration for the blessing.")
	cmdVrun.Flags.StringVar(&name, "name", "", "Name to use for the blessing. Uses the command name if unset.")
	cmdVrun.Flags.StringVar(&role, "role", "", "Role object from which to request the blessing. If set, the blessings from this role server are used and --name is ignored. If not set, the default blessings of the calling principal are extended with --name.")

	os.Exit(cmdVrun.Main())
}

func vrun(cmd *cmdline.Command, args []string) error {
	ctx, shutdown := v23.Init()
	defer shutdown()

	if len(args) == 0 {
		return cmd.UsageErrorf("vrun: no command specified")
	}
	principal, conn, err := createPrincipal(ctx)
	if err != nil {
		return err
	}
	if len(role) == 0 {
		if len(name) == 0 {
			name = filepath.Base(args[0])
		}
		if err := bless(ctx, principal, name); err != nil {
			return err
		}
	} else {
		// The role server expects the client's blessing name to end
		// with RoleSuffix. This is to avoid accidentally granting role
		// access to anything else that might have been blessed by the
		// same principal.
		if err := bless(ctx, principal, isecurity.RoleSuffix); err != nil {
			return err
		}
		rCtx, err := v23.SetPrincipal(ctx, principal)
		if err != nil {
			return err
		}
		if err := setupRoleBlessings(rCtx, role); err != nil {
			return err
		}
	}

	return doExec(args, conn)
}

func bless(ctx *context.T, p security.Principal, name string) error {
	caveat, err := security.ExpiryCaveat(time.Now().Add(durationFlag))
	if err != nil {
		vlog.Errorf("Couldn't create caveat")
		return err
	}

	rp := v23.GetPrincipal(ctx)
	blessing, err := rp.Bless(p.PublicKey(), rp.BlessingStore().Default(), name, caveat)
	if err != nil {
		vlog.Errorf("Couldn't bless")
		return err
	}

	if err = p.BlessingStore().SetDefault(blessing); err != nil {
		vlog.Errorf("Couldn't set default blessing")
		return err
	}
	if _, err = p.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
		vlog.Errorf("Couldn't set default client blessing")
		return err
	}
	if err = p.AddToRoots(blessing); err != nil {
		vlog.Errorf("Couldn't set trusted roots")
		return err
	}
	return nil
}

func doExec(cmd []string, conn *os.File) error {
	os.Setenv(consts.VeyronCredentials, "")
	os.Setenv(agent.FdVarName, "3")
	if conn.Fd() != 3 {
		if err := syscall.Dup2(int(conn.Fd()), 3); err != nil {
			vlog.Errorf("Couldn't dup fd")
			return err
		}
		conn.Close()
	}
	err := syscall.Exec(cmd[0], cmd, os.Environ())
	vlog.Errorf("Couldn't exec %s.", cmd[0])
	return err
}

func createPrincipal(ctx *context.T) (security.Principal, *os.File, error) {
	kagent, err := keymgr.NewAgent()
	if err != nil {
		vlog.Errorf("Could not initialize agent")
		return nil, nil, err
	}

	_, conn, err := kagent.NewPrincipal(ctx, true)
	if err != nil {
		vlog.Errorf("Couldn't create principal")
		return nil, nil, err
	}

	// Connect to the Principal
	fd, err := syscall.Dup(int(conn.Fd()))
	if err != nil {
		vlog.Errorf("Couldn't copy fd")
		return nil, nil, err
	}
	syscall.CloseOnExec(fd)
	principal, err := agent.NewAgentPrincipal(ctx, fd, v23.GetClient(ctx))
	if err != nil {
		vlog.Errorf("Couldn't connect to principal")
		return nil, nil, err
	}
	return principal, conn, nil
}

func setupRoleBlessings(ctx *context.T, role string) error {
	b, err := isecurity.RoleClient(role).SeekBlessings(ctx)
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

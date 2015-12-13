// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The following enables go generate to generate the doc.go file.
//go:generate go run $JIRI_ROOT/release/go/src/v.io/x/lib/cmdline/testdata/gendoc.go . -help

package main

import (
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
	"v.io/x/lib/vlog"
	"v.io/x/ref"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/services/agent"
	"v.io/x/ref/services/agent/agentlib"
	"v.io/x/ref/services/agent/keymgr"
	"v.io/x/ref/services/role"

	_ "v.io/x/ref/runtime/factories/generic"
)

var (
	durationFlag time.Duration
	nameFlag     string
	roleFlag     string
	undeprecate  bool
)

var cmdVrun = &cmdline.Command{
	Runner:   v23cmd.RunnerFunc(vrun),
	Name:     "vrun",
	Short:    "executes commands with a derived Vanadium principal",
	Long:     "Command vrun executes commands with a derived Vanadium principal.",
	ArgsName: "<command> [command args...]",
}

func main() {
	cmdline.HideGlobalFlagsExcept()
	syscall.CloseOnExec(3)
	syscall.CloseOnExec(4)

	cmdVrun.Flags.DurationVar(&durationFlag, "duration", 1*time.Hour, "Duration for the blessing.")
	cmdVrun.Flags.StringVar(&nameFlag, "name", "", "Name to use for the blessing. Uses the command name if unset.")
	cmdVrun.Flags.StringVar(&roleFlag, "role", "", "Role object from which to request the blessing. If set, the blessings from this role server are used and --name is ignored. If not set, the default blessings of the calling principal are extended with --name.")
	cmdVrun.Flags.BoolVar(&undeprecate, "i-really-need-vrun", false, "If you really need to use vrun because vbecome doesn't work for your use case, set this flag.  If you do, please let {ashankar,caprita}@google.com know about your use case.  Note that this is temporary, as the vrun tool will be removed at some point in the future.")

	cmdline.Main(cmdVrun)
}

func vrun(ctx *context.T, env *cmdline.Env, args []string) error {
	if undeprecate {
		fmt.Fprintf(env.Stderr, "WARNING: vrun will be deprecated soon.  Please let {ashankar,caprita}@google.com know why you still need it.\n")
	} else {
		return env.UsageErrorf("the vrun tool is deprecated, please use vbecome instead.  If you really need vrun, see the --i-really-need-vrun flag.")
	}

	if len(args) == 0 {
		args = []string{"bash", "--norc"}
	}
	m, err := connectToKeyManager()
	if err != nil {
		return err
	}

	path, err := newPrincipal(m)
	if err != nil {
		return err
	}

	// Connect to the Principal
	principal, err := agentlib.NewAgentPrincipalX(path)
	if err != nil {
		vlog.Errorf("Couldn't connect to principal")
	}

	if len(roleFlag) == 0 {
		if len(nameFlag) == 0 {
			nameFlag = filepath.Base(args[0])
		}
		if err := bless(ctx, principal, nameFlag); err != nil {
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
		rCtx, err := v23.WithPrincipal(ctx, principal)
		if err != nil {
			return err
		}
		if err := setupRoleBlessings(rCtx, roleFlag); err != nil {
			return err
		}
	}

	return doExec(args, path)
}

func bless(ctx *context.T, p security.Principal, name string) error {
	caveat, err := security.NewExpiryCaveat(time.Now().Add(durationFlag))
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
	if err = security.AddToRoots(p, blessing); err != nil {
		vlog.Errorf("Couldn't set trusted roots")
		return err
	}
	return nil
}

func doExec(cmd []string, agentPath string) error {
	ref.EnvClearCredentials()
	if err := os.Setenv(ref.EnvAgentPath, agentPath); err != nil {
		return err
	}
	p, err := exec.LookPath(cmd[0])
	if err != nil {
		vlog.Errorf("Couldn't find %q", cmd[0])
		return err
	}
	err = syscall.Exec(p, cmd, os.Environ())
	vlog.Errorf("Couldn't exec %s.", cmd[0])
	return err
}

func connectToKeyManager() (agent.KeyManager, error) {
	path := os.Getenv(ref.EnvAgentPath)
	return keymgr.NewKeyManager(path)
}

func newPrincipal(m agent.KeyManager) (path string, err error) {
	var dir string
	if dir, err = ioutil.TempDir("", "vrun"); err != nil {
		return
	}
	var id [64]byte
	if id, err = m.NewPrincipal(true); err != nil {
		vlog.Errorf("Couldn't create principal")
		return
	}
	// Note: because we exec the child, there's no way to cleanup
	// this principal and socket after the child is gone.
	path = filepath.Join(dir, "sock")
	err = m.ServePrincipal(id, path)
	return
}

func setupRoleBlessings(ctx *context.T, roleStr string) error {
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

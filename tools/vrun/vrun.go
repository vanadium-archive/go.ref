package main

import (
	"flag"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"v.io/lib/cmdline"
	"v.io/veyron/veyron/lib/flags/consts"
	"v.io/veyron/veyron/security/agent"
	"v.io/veyron/veyron/security/agent/keymgr"

	"v.io/veyron/veyron2"
	"v.io/veyron/veyron2/options"
	"v.io/veyron/veyron2/rt"
	"v.io/veyron/veyron2/security"
	"v.io/veyron/veyron2/vlog"

	_ "v.io/veyron/veyron/profiles"
)

var runtime veyron2.Runtime
var durationFlag time.Duration
var nameOverride string

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

	flag.DurationVar(&durationFlag, "duration", 1*time.Hour, "Duration for the blessing.")
	flag.StringVar(&nameOverride, "name", "", "Name to use for the blessing. Uses the command name if unset.")
	cmdVrun.Flags.DurationVar(&durationFlag, "duration", 1*time.Hour, "Duration for the blessing.")
	cmdVrun.Flags.StringVar(&nameOverride, "name", "", "Name to use for the blessing. Uses the command name if unset.")

	cmdVrun.Main()
}

func vrun(cmd *cmdline.Command, args []string) error {
	var err error
	runtime, err = rt.New()
	if err != nil {
		vlog.Errorf("Could not initialize runtime")
		return err
	}
	defer runtime.Cleanup()

	if len(args) == 0 {
		return cmd.UsageErrorf("vrun: no command specified")
	}
	principal, conn, err := createPrincipal()
	if err != nil {
		return err
	}
	err = bless(principal, filepath.Base(args[0]))
	if err != nil {
		return err
	}
	return doExec(args, conn)
}

func bless(p security.Principal, name string) error {
	caveat, err := security.ExpiryCaveat(time.Now().Add(durationFlag))
	if err != nil {
		vlog.Errorf("Couldn't create caveat")
		return err
	}
	if 0 != len(nameOverride) {
		name = nameOverride
	}

	blessing, err := runtime.Principal().Bless(p.PublicKey(), runtime.Principal().BlessingStore().Default(), name, caveat)
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

func createPrincipal() (security.Principal, *os.File, error) {
	kagent, err := keymgr.NewAgent()
	if err != nil {
		vlog.Errorf("Could not initialize agent")
		return nil, nil, err
	}

	_, conn, err := kagent.NewPrincipal(runtime.NewContext(), true)
	if err != nil {
		vlog.Errorf("Couldn't create principal")
		return nil, nil, err
	}

	// Connect to the Principal
	client, err := runtime.NewClient(options.VCSecurityNone)
	if err != nil {
		vlog.Errorf("Couldn't create client")
		return nil, nil, err
	}
	fd, err := syscall.Dup(int(conn.Fd()))
	if err != nil {
		vlog.Errorf("Couldn't copy fd")
		return nil, nil, err
	}
	syscall.CloseOnExec(fd)
	principal, err := agent.NewAgentPrincipal(client, fd, runtime.NewContext())
	if err != nil {
		vlog.Errorf("Couldn't connect to principal")
		return nil, nil, err
	}
	return principal, conn, nil
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"

	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

type suidHelperState struct {
	dmUser     string // user that the device manager is running as
	helperPath string // path to the suidhelper binary
}

var suidHelper *suidHelperState

func initSuidHelper(helperPath string) {
	if suidHelper != nil || helperPath == "" {
		return
	}

	u, err := user.Current()
	if err != nil {
		vlog.Panicf("devicemanager has no current user: %v", err)
	}
	suidHelper = &suidHelperState{
		dmUser:     u.Username,
		helperPath: helperPath,
	}
}

// terminatePid sends a SIGKILL to the target pid
func (s suidHelperState) terminatePid(pid int, stdout, stderr io.Writer) error {
	if err := s.internalModalOp(stdout, stderr, "--kill", strconv.Itoa(pid)); err != nil {
		return fmt.Errorf("devicemanager's invocation of suidhelper to kill pid %v failed: %v", pid, err)
	}
	return nil
}

// deleteFileTree deletes a file or directory
func (s suidHelperState) deleteFileTree(dirOrFile string, stdout, stderr io.Writer) error {
	if err := s.internalModalOp(stdout, stderr, "--rm", dirOrFile); err != nil {
		return fmt.Errorf("devicemanager's invocation of suidhelper delete %v failed: %v", dirOrFile, err)
	}
	return nil
}

type suidAppCmdArgs struct {
	// args to helper
	targetUser, progname, workspace, logdir, binpath string
	// fields in exec.Cmd
	env            []string
	stdout, stderr io.Writer
	dir            string
	// arguments passed to app
	appArgs []string
}

// getAppCmd produces an exec.Cmd that can be used to start an app
func (s suidHelperState) getAppCmd(a *suidAppCmdArgs) (*exec.Cmd, error) {
	if a.targetUser == "" || a.progname == "" || a.binpath == "" || a.workspace == "" || a.logdir == "" {
		return nil, fmt.Errorf("Invalid args passed to getAppCmd: %+v", a)
	}

	cmd := exec.Command(s.helperPath)

	switch yes, err := s.suidhelperEnabled(a.targetUser); {
	case err != nil:
		return nil, err
	case yes:
		cmd.Args = append(cmd.Args, "--username", a.targetUser)
	case !yes:
		cmd.Args = append(cmd.Args, "--username", a.targetUser, "--dryrun")
	}

	cmd.Args = append(cmd.Args, "--progname", a.progname)
	cmd.Args = append(cmd.Args, "--workspace", a.workspace)
	cmd.Args = append(cmd.Args, "--logdir", a.logdir)

	cmd.Args = append(cmd.Args, "--run", a.binpath)
	cmd.Args = append(cmd.Args, "--")

	cmd.Args = append(cmd.Args, a.appArgs...)

	cmd.Env = a.env
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr
	cmd.Dir = a.dir

	return cmd, nil
}

// internalModalOp is a convenience routine containing the common part of all
// modal operations. Only other routines implementing specific suidhelper operations
// (like terminatePid and deleteFileTree) should call this directly.
func (s suidHelperState) internalModalOp(stdout, stderr io.Writer, arg ...string) error {
	cmd := exec.Command(s.helperPath)
	cmd.Args = append(cmd.Args, arg...)
	if stderr != nil {
		cmd.Stderr = stderr
	}
	if stdout != nil {
		cmd.Stdout = stdout
	}

	if err := cmd.Run(); err != nil {
		vlog.Errorf("failed calling helper with args (%v):%v", arg, err)
	}
	return nil
}

// isSetuid is defined like this so we can override its
// implementation for tests.
var isSetuid = func(fileStat os.FileInfo) bool {
	vlog.VI(2).Infof("running the original isSetuid")
	return fileStat.Mode()&os.ModeSetuid == os.ModeSetuid
}

// suidhelperEnabled determines if the suidhelper must exist and be
// setuid to run an application as system user targetUser. If false, the
// setuidhelper must be invoked with the --dryrun flag to skip making
// system calls that will fail or provide apps with a trivial
// priviledge escalation.
func (s suidHelperState) suidhelperEnabled(targetUser string) (bool, error) {
	helperStat, err := os.Stat(s.helperPath)
	if err != nil {
		return false, verror.New(ErrOperationFailed, nil, fmt.Sprintf("Stat(%v) failed: %v. helper is required.", s.helperPath, err))
	}
	haveHelper := isSetuid(helperStat)

	switch {
	case haveHelper && s.dmUser != targetUser:
		return true, nil
	case haveHelper && s.dmUser == targetUser:
		return false, verror.New(verror.ErrNoAccess, nil, fmt.Sprintf("suidhelperEnabled failed: %q == %q", s.dmUser, targetUser))
	default:
		return false, nil
	}
	// Keep the compiler happy.
	return false, nil
}

// usernameForPrincipal returns the system name that the
// devicemanager will use to invoke apps.
// TODO(rjkroege): This code assumes a desktop target and will need
// to be reconsidered for embedded contexts.
func (s suidHelperState) usernameForPrincipal(ctx *context.T, uat BlessingSystemAssociationStore) string {
	identityNames, _ := security.RemoteBlessingNames(ctx)
	systemName, present := uat.SystemAccountForBlessings(identityNames)

	if present {
		return systemName
	} else {
		return s.dmUser
	}
}

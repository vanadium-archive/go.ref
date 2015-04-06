// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package suid

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/user"
	"strconv"
	"strings"

	"v.io/v23/verror"
)

const pkgPath = "v.io/x/ref/lib/suid"

var (
	errUserNameMissing = verror.Register(pkgPath+".errUserNameMissing", verror.NoRetry, "{1:}{2:} --username missing{:_}")
	errUnknownUser     = verror.Register(pkgPath+".errUnknownUser", verror.NoRetry, "{1:}{2:} --username {3}: unknown user{:_}")
	errInvalidUID      = verror.Register(pkgPath+".errInvalidUID", verror.NoRetry, "{1:}{2:} user.Lookup() returned an invalid uid {3}{:_}")
	errInvalidGID      = verror.Register(pkgPath+".errInvalidGID", verror.NoRetry, "{1:}{2:} user.Lookup() returned an invalid gid {3}{:_}")
	errUIDTooLow       = verror.Register(pkgPath+".errUIDTooLow", verror.NoRetry, "{1:}{2:} suidhelper uid {3} is not permitted because it is less than {4}{:_}")
)

type WorkParameters struct {
	uid       int
	gid       int
	workspace string
	logDir    string
	argv0     string
	argv      []string
	envv      []string
	dryrun    bool
	remove    bool
}

type ArgsSavedForTest struct {
	Uname    string
	Workpace string
	Run      string
	LogDir   string
}

const SavedArgs = "V23_SAVED_ARGS"

var (
	flagUsername, flagWorkspace, flagLogDir, flagRun, flagProgName *string
	flagMinimumUid                                                 *int64
	flagRemove, flagDryrun                                         *bool
)

func init() {
	setupFlags(flag.CommandLine)
}

func setupFlags(fs *flag.FlagSet) {
	const uidThreshold = 501
	flagUsername = fs.String("username", "", "The UNIX user name used for the other functions of this tool.")
	flagWorkspace = fs.String("workspace", "", "Path to the application's workspace directory.")
	flagLogDir = fs.String("logdir", "", "Path to the log directory.")
	flagRun = fs.String("run", "", "Path to the application to exec.")
	flagProgName = fs.String("progname", "unnamed_app", "Visible name of the application, used in argv[0]")
	flagMinimumUid = fs.Int64("minuid", uidThreshold, "UIDs cannot be less than this number.")
	flagRemove = fs.Bool("rm", false, "Remove the file trees given as command-line arguments.")
	flagDryrun = fs.Bool("dryrun", false, "Elides root-requiring systemcalls.")
}

func cleanEnv(env []string) []string {
	nenv := []string{}
	for _, e := range env {
		if !strings.HasPrefix(e, "V23_SUIDHELPER_TEST") {
			nenv = append(nenv, e)
		}
	}
	return nenv
}

// ParseArguments populates the WorkParameter object from the provided args
// and env strings.
func (wp *WorkParameters) ProcessArguments(fs *flag.FlagSet, env []string) error {
	if *flagRemove {
		wp.remove = true
		wp.argv = fs.Args()
		return nil
	}

	username := *flagUsername
	if username == "" {
		return verror.New(errUserNameMissing, nil)
	}

	usr, err := user.Lookup(username)
	if err != nil {
		return verror.New(errUnknownUser, nil, username)
	}

	uid, err := strconv.ParseInt(usr.Uid, 0, 32)
	if err != nil {
		return verror.New(errInvalidUID, nil, usr.Uid)
	}
	gid, err := strconv.ParseInt(usr.Gid, 0, 32)
	if err != nil {
		return verror.New(errInvalidGID, nil, usr.Gid)
	}

	// Uids less than 501 can be special so we forbid running as them.
	if uid < *flagMinimumUid {
		return verror.New(errUIDTooLow, nil,
			uid, *flagMinimumUid)
	}

	wp.dryrun = *flagDryrun

	// Preserve the arguments for examination by the test harness if executed
	// in the course of a test.
	if os.Getenv("V23_SUIDHELPER_TEST") != "" {
		env = cleanEnv(env)
		b := new(bytes.Buffer)
		enc := json.NewEncoder(b)
		enc.Encode(ArgsSavedForTest{
			Uname:    *flagUsername,
			Workpace: *flagWorkspace,
			Run:      *flagRun,
			LogDir:   *flagLogDir,
		})
		env = append(env, SavedArgs+"="+b.String())
		wp.dryrun = true
	}

	wp.uid = int(uid)
	wp.gid = int(gid)
	wp.workspace = *flagWorkspace
	wp.argv0 = *flagRun
	wp.logDir = *flagLogDir
	wp.argv = append([]string{*flagProgName}, fs.Args()...)
	// TODO(rjkroege): Reduce the environment to the absolute minimum needed.
	wp.envv = env

	return nil
}

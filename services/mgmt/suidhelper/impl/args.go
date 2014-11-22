package impl

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"strconv"

	sflag "v.io/core/veyron/services/mgmt/suidhelper/impl/flag"
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

const SavedArgs = "VEYRON_SAVED_ARGS"

var (
	flagUsername, flagWorkspace, flagLogDir, flagRun *string
	flagMinimumUid                                   *int64
	flagRemove, flagDryrun                           *bool
)

func init() {
	setupFlags(nil)
}

func setupFlags(fs *flag.FlagSet) {
	if fs != nil {
		sflag.SetupFlags(fs)
	}
	flagUsername = sflag.Username
	flagWorkspace = sflag.Workspace
	flagLogDir = sflag.LogDir
	flagRun = sflag.Run
	flagMinimumUid = sflag.MinimumUid
	flagRemove = sflag.Remove
	flagDryrun = sflag.Dryrun
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
		return fmt.Errorf("--username missing")
	}

	usr, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("--username %s: unknown user", username)
	}

	uid, err := strconv.ParseInt(usr.Uid, 0, 32)
	if err != nil {
		return fmt.Errorf("user.Lookup() returned an invalid uid %v", usr.Uid)
	}
	gid, err := strconv.ParseInt(usr.Gid, 0, 32)
	if err != nil {
		return fmt.Errorf("user.Lookup() returned an invalid gid %v", usr.Gid)
	}

	// Uids less than 501 can be special so we forbid running as them.
	if uid < *flagMinimumUid {
		return fmt.Errorf("suidhelper does not permit uids less than %d", *flagMinimumUid)
	}

	wp.dryrun = *flagDryrun

	// Preserve the arguments for examination by the test harness if executed
	// in the course of a test.
	if os.Getenv("VEYRON_SUIDHELPER_TEST") != "" {
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
	wp.argv = append([]string{wp.argv0}, fs.Args()...)
	// TODO(rjkroege): Reduce the environment to the absolute minimum needed.
	wp.envv = env

	return nil
}

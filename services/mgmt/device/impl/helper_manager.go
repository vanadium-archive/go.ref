package impl

import (
	"os"
	"os/user"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"
)

type suidHelperState string

var suidHelper suidHelperState

func init() {
	u, err := user.Current()
	if err != nil {
		vlog.Panicf("devicemanager has no current user: %v", err)
	}
	suidHelper = suidHelperState(u.Username)
}

// isSetuid is defined like this so we can override its
// implementation for tests.
var isSetuid = func(fileStat os.FileInfo) bool {
	vlog.VI(2).Infof("running the original isSetuid")
	return fileStat.Mode()&os.ModeSetuid == os.ModeSetuid
}

// unameRequiresSuidhelper determines if the the suidhelper must exist
// and be setuid to run an application as system user un. If false, the
// setuidhelper must be invoked with the --dryrun flag to skip making
// system calls that will fail or provide apps with a trivial
// priviledge escalation.
func (dn suidHelperState) suidhelperEnabled(un, helperPath string) (bool, error) {
	helperStat, err := os.Stat(helperPath)
	if err != nil {
		vlog.Errorf("Stat(%v) failed: %v. helper is required.", helperPath, err)
		return false, verror2.Make(ErrOperationFailed, nil)
	}
	haveHelper := isSetuid(helperStat)

	switch {
	case haveHelper && string(dn) != un:
		return true, nil
	case haveHelper && string(dn) == un:
		return false, verror2.Make(verror2.NoAccess, nil)
	default:
		return false, nil
	}
	// Keep the compiler happy.
	return false, nil
}

// usernameForVanadiumPrincipal returns the system name that the
// devicemanager will use to invoke apps.
// TODO(rjkroege): This code assumes a desktop target and will need
// to be reconsidered for embedded contexts.
func (i suidHelperState) usernameForPrincipal(ctx ipc.ServerContext, uat BlessingSystemAssociationStore) string {
	identityNames, _ := ctx.RemoteBlessings().ForContext(ctx)
	systemName, present := uat.SystemAccountForBlessings(identityNames)

	if present {
		return systemName
	} else {
		return string(i)
	}
}

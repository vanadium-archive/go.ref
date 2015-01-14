package rt

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/mgmt"
	"v.io/core/veyron2/security"

	"v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/stats"
	vsecurity "v.io/core/veyron/security"
	"v.io/core/veyron/security/agent"
)

func initSecurity(ctx *context.T, handle *exec.ChildHandle, credentials string) (security.Principal, error) {
	principal, err := setupPrincipal(ctx, handle, credentials)
	if err != nil {
		return nil, err
	}

	// TODO(suharshs,mattr): Move this code to SetNewPrincipal and determine what their string should be.
	stats.NewString("security/principal/key").Set(principal.PublicKey().String())
	stats.NewStringFunc("security/principal/blessingstore", principal.BlessingStore().DebugString)
	stats.NewStringFunc("security/principal/blessingroots", principal.Roots().DebugString)
	return principal, nil
}

func setupPrincipal(ctx *context.T, handle *exec.ChildHandle, credentials string) (security.Principal, error) {
	var err error
	var principal security.Principal
	if principal, _ = ctx.Value(principalKey).(security.Principal); principal != nil {
		return principal, nil
	}
	if fd, err := agentFD(handle); err != nil {
		return nil, err
	} else if fd >= 0 {
		return agent.NewAgentPrincipal(ctx, fd)
	}
	if len(credentials) > 0 {
		// TODO(ataly, ashankar): If multiple runtimes are getting
		// initialized at the same time from the same VEYRON_CREDENTIALS
		// we will need some kind of locking for the credential files.
		if principal, err = vsecurity.LoadPersistentPrincipal(credentials, nil); err != nil {
			if os.IsNotExist(err) {
				if principal, err = vsecurity.CreatePersistentPrincipal(credentials, nil); err != nil {
					return principal, err
				}
				return principal, vsecurity.InitDefaultBlessings(principal, defaultBlessingName())
			}
			return nil, err
		}
		return principal, nil
	}
	if principal, err = vsecurity.NewPrincipal(); err != nil {
		return principal, err
	}
	return principal, vsecurity.InitDefaultBlessings(principal, defaultBlessingName())
}

// agentFD returns a non-negative file descriptor to be used to communicate with
// the security agent if the current process has been configured to use the
// agent.
func agentFD(handle *exec.ChildHandle) (int, error) {
	var fd string
	if handle != nil {
		// We were started by a parent (presumably, device manager).
		fd, _ = handle.Config.Get(mgmt.SecurityAgentFDConfigKey)
	} else {
		fd = os.Getenv(agent.FdVarName)
	}
	if fd == "" {
		return -1, nil
	}
	return strconv.Atoi(fd)
}

func defaultBlessingName() string {
	var name string
	if user, _ := user.Current(); user != nil && len(user.Username) > 0 {
		name = user.Username
	} else {
		name = "anonymous"
	}
	if host, _ := os.Hostname(); len(host) > 0 {
		name = name + "@" + host
	}
	return fmt.Sprintf("%s-%d", name, os.Getpid())
}

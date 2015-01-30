package rt

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/mgmt"
	"v.io/core/veyron2/security"

	"v.io/core/veyron/lib/exec"
	vsecurity "v.io/core/veyron/security"
	"v.io/core/veyron/security/agent"
)

func initSecurity(ctx *context.T, credentials string, client ipc.Client) (security.Principal, error) {
	principal, err := setupPrincipal(ctx, credentials, client)
	if err != nil {
		return nil, err
	}

	return principal, nil
}

func setupPrincipal(ctx *context.T, credentials string, client ipc.Client) (security.Principal, error) {
	var err error
	var principal security.Principal
	if principal, _ = ctx.Value(principalKey).(security.Principal); principal != nil {
		return principal, nil
	}
	if fd, err := agentFD(); err != nil {
		return nil, err
	} else if fd >= 0 {
		return connectToAgent(ctx, fd, client)
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
func agentFD() (int, error) {
	handle, err := exec.GetChildHandle()
	if err != nil && err != exec.ErrNoVersion {
		return -1, err
	}
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
	ifd, err := strconv.Atoi(fd)
	if err == nil && handle != nil {
		// If we're using a handle, children can't inherit the agent.
		syscall.CloseOnExec(ifd)
	}
	return ifd, err
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

func connectToAgent(ctx *context.T, fd int, client ipc.Client) (security.Principal, error) {
	// Dup the fd, so we can create multiple runtimes.
	syscall.ForkLock.Lock()
	newfd, err := syscall.Dup(fd)
	if err == nil {
		syscall.CloseOnExec(newfd)
	}
	syscall.ForkLock.Unlock()
	if err != nil {
		return nil, err
	}
	return agent.NewAgentPrincipal(ctx, newfd, client)
}

package rt

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"v.io/core/veyron2/mgmt"
	"v.io/core/veyron2/options"
	"v.io/core/veyron2/security"

	"v.io/core/veyron/lib/exec"
	"v.io/core/veyron/lib/stats"
	vsecurity "v.io/core/veyron/security"
	"v.io/core/veyron/security/agent"
)

func (rt *vrt) Principal() security.Principal {
	return rt.principal
}

func (rt *vrt) initSecurity(handle *exec.ChildHandle, credentials string) error {
	if err := rt.setupPrincipal(handle, credentials); err != nil {
		return err
	}
	stats.NewString("security/principal/key").Set(rt.principal.PublicKey().String())
	stats.NewStringFunc("security/principal/blessingstore", rt.principal.BlessingStore().DebugString)
	stats.NewStringFunc("security/principal/blessingroots", rt.principal.Roots().DebugString)
	return nil
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

func (rt *vrt) setupPrincipal(handle *exec.ChildHandle, credentials string) error {
	if rt.principal != nil {
		return nil
	}
	if fd, err := agentFD(handle); err != nil {
		return err
	} else if fd >= 0 {
		var err error
		rt.principal, err = rt.connectToAgent(fd)
		return err
	}
	if len(credentials) > 0 {
		// TODO(ataly, ashankar): If multiple runtimes are getting
		// initialized at the same time from the same VEYRON_CREDENTIALS
		// we will need some kind of locking for the credential files.
		var err error
		if rt.principal, err = vsecurity.LoadPersistentPrincipal(credentials, nil); err != nil {
			if os.IsNotExist(err) {
				if rt.principal, err = vsecurity.CreatePersistentPrincipal(credentials, nil); err != nil {
					return err
				}
				return vsecurity.InitDefaultBlessings(rt.principal, defaultBlessingName())
			}
			return err
		}
		return nil
	}
	var err error
	if rt.principal, err = vsecurity.NewPrincipal(); err != nil {
		return err
	}
	return vsecurity.InitDefaultBlessings(rt.principal, defaultBlessingName())
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

func (rt *vrt) connectToAgent(fd int) (security.Principal, error) {
	client, err := rt.NewClient(options.VCSecurityNone)
	if err != nil {
		return nil, err
	}
	return agent.NewAgentPrincipal(client, fd, rt.NewContext())
}

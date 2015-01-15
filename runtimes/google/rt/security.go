package rt

import (
	"os"
	"syscall"

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

func (rt *vrt) connectToAgent(fd int) (security.Principal, error) {
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
	return agent.NewAgentPrincipal(rt.NewContext(), newfd)
}

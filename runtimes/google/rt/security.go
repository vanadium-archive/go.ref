package rt

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"veyron.io/veyron/veyron/lib/stats"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/agent"

	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/security"
)

func (rt *vrt) Principal() security.Principal {
	return rt.principal
}

func (rt *vrt) initSecurity(credentials string) error {
	if err := rt.setupPrincipal(credentials); err != nil {
		return err
	}
	stats.NewString("security/principal/key").Set(rt.principal.PublicKey().String())
	stats.NewStringFunc("security/principal/blessingstore", rt.principal.BlessingStore().DebugString)
	stats.NewStringFunc("security/principal/blessingroots", rt.principal.Roots().DebugString)
	return nil
}

func (rt *vrt) setupPrincipal(credentials string) error {
	if rt.principal != nil {
		return nil
	}
	var err error
	// TODO(cnicolaou,ashankar,ribrdb): this should be supplied via
	// the exec.GetChildHandle call.
	if len(os.Getenv(agent.FdVarName)) > 0 {
		rt.principal, err = rt.connectToAgent()
		return err
	} else if len(credentials) > 0 {
		// TODO(ataly, ashankar): If multiple runtimes are getting
		// initialized at the same time from the same VEYRON_CREDENTIALS
		// we will need some kind of locking for the credential files.
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

func (rt *vrt) connectToAgent() (security.Principal, error) {
	client, err := rt.NewClient(options.VCSecurityNone)
	if err != nil {
		return nil, err
	}
	fd, err := strconv.Atoi(os.Getenv(agent.FdVarName))
	if err != nil {
		return nil, err
	}
	return agent.NewAgentPrincipal(client, fd, rt.NewContext())
}

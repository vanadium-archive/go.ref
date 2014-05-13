package rt

import (
	"fmt"
	"os"
	"os/user"

	isecurity "veyron/runtimes/google/security"

	"veyron2/security"
	"veyron2/vlog"
)

func (rt *vrt) NewIdentity(name string) (security.PrivateID, error) {
	return isecurity.NewPrivateID(name)
}

func (rt *vrt) Identity() security.PrivateID {
	return rt.id.PrivateID
}

func (rt *vrt) initIdentity() error {
	if rt.id.PrivateID == nil {
		var err error
		if file := os.Getenv("VEYRON_IDENTITY"); len(file) > 0 {
			if rt.id.PrivateID, err = loadIdentityFromFile(file); err != nil {
				vlog.Errorf("Could not load identity from %q", file)
				return err
			}
		} else {
			name := defaultIdentityName()
			vlog.VI(2).Infof("No identity provided to the runtime, minting one for %q", name)
			if rt.id.PrivateID, err = rt.NewIdentity(name); err != nil {
				vlog.Errorf("Could not create new identity: %v", err)
				return err
			}
		}
	}
	// Always trust our own identity providers.
	trustIdentityProviders(rt.id.PrivateID)
	return nil
}

func defaultIdentityName() string {
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

func trustIdentityProviders(id security.PrivateID) { isecurity.TrustIdentityProviders(id) }

func loadIdentityFromFile(filePath string) (security.PrivateID, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return security.LoadIdentity(f)
}

package rt

import (
	"fmt"
	"os"
	"os/user"
	"sync"

	isecurity "veyron/runtimes/google/security"

	"veyron2/security"
	"veyron2/vlog"
)

// currentIDOpt is an option that can be used to pass the identity currently used
// by the runtime to an ipc.Server or ipc.StreamListener.
type currentIDOpt struct {
	id security.PrivateID
	mu sync.RWMutex
}

func (id *currentIDOpt) Identity() security.PrivateID {
	id.mu.RLock()
	defer id.mu.RUnlock()
	return id.id
}

func (id *currentIDOpt) setIdentity(newID security.PrivateID) {
	// TODO(ataly): Whenever setIdentity is invoked on the identity currently used by
	// the runtime, the following changes must also be performed:
	// * the identity provider of the new identity must be tursted.
	// * the default client used by the runtime must also be replaced with
	//   a client using the new identity.
	id.mu.Lock()
	defer id.mu.Unlock()
	id.id = newID
}

func (*currentIDOpt) IPCServerOpt() {}

func (*currentIDOpt) IPCStreamListenerOpt() {}

func (rt *vrt) NewIdentity(name string) (security.PrivateID, error) {
	return isecurity.NewPrivateID(name)
}

func (rt *vrt) Identity() security.PrivateID {
	return rt.id.Identity()
}

func (rt *vrt) initIdentity() error {
	if rt.id.Identity() == nil {
		var id security.PrivateID
		var err error
		if file := os.Getenv("VEYRON_IDENTITY"); len(file) > 0 {
			if id, err = loadIdentityFromFile(file); err != nil {
				return fmt.Errorf("Could not load identity from %q: %v", file, err)
			}
		} else {
			name := defaultIdentityName()
			vlog.VI(2).Infof("No identity provided to the runtime, minting one for %q", name)
			if id, err = rt.NewIdentity(name); err != nil {
				return fmt.Errorf("Could not create new identity: %v", err)
			}
		}
		rt.id.setIdentity(id)
	}
	// Always trust our own identity providers.
	trustIdentityProviders(rt.id.Identity())
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

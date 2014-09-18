package rt

import (
	"fmt"
	"os"
	"os/user"

	"veyron.io/veyron/veyron/lib/unixfd"
	isecurity "veyron.io/veyron/veyron/runtimes/google/security"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/agent"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"
)

func (rt *vrt) NewIdentity(name string) (security.PrivateID, error) {
	return isecurity.NewPrivateID(name, nil)
}

func (rt *vrt) Identity() security.PrivateID {
	return rt.id
}

func (rt *vrt) PublicIDStore() security.PublicIDStore {
	return rt.store
}

func (rt *vrt) initSecurity() error {
	if err := rt.initIdentity(); err != nil {
		return err
	}
	if err := rt.initPublicIDStore(); err != nil {
		return err
	}
	// Initialize the runtime's PublicIDStore with the runtime's PublicID.
	// TODO(ashankar,ataly): What should be the tag for the PublicID? Below we use
	// security.AllPrincipals but this means that the PublicID *always* gets used
	// for any peer. This may not be desirable.
	if err := rt.store.Add(rt.id.PublicID(), security.AllPrincipals); err != nil {
		return fmt.Errorf("could not initialize a PublicIDStore for the runtime: %s", err)
	}

	// Always trust our own identity providers.
	// TODO(ataly, ashankar): We should trust the identity providers of all PublicIDs in the store.
	trustIdentityProviders(rt.id)
	return nil
}

func (rt *vrt) initIdentity() error {
	if rt.id != nil {
		return nil
	}
	var err error
	if len(os.Getenv(agent.EndpointVarName)) > 0 {
		rt.id, err = rt.connectToAgent()
		return err
	} else if file := os.Getenv("VEYRON_IDENTITY"); len(file) > 0 {
		if rt.id, err = loadIdentityFromFile(file); err != nil || rt.id == nil {
			return fmt.Errorf("Could not load identity from the VEYRON_IDENTITY environment variable (%q): %v", file, err)
		}
	} else {
		name := defaultIdentityName()
		vlog.VI(2).Infof("No identity provided to the runtime, minting one for %q", name)
		if rt.id, err = rt.NewIdentity(name); err != nil || rt.id == nil {
			return fmt.Errorf("Could not create new identity: %v", err)
		}
	}
	return nil
}

func (rt *vrt) initPublicIDStore() error {
	if rt.store != nil {
		return nil
	}

	var backup *isecurity.PublicIDStoreParams
	// TODO(ataly): Get rid of this environment variable and have a single variable for
	// all security related initialization.
	if dir := os.Getenv("VEYRON_PUBLICID_STORE"); len(dir) > 0 {
		backup = &isecurity.PublicIDStoreParams{dir, rt.id}
	}
	var err error
	if rt.store, err = isecurity.NewPublicIDStore(backup); err != nil {
		return fmt.Errorf("Could not create PublicIDStore for runtime: %v", err)
	}
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
	// TODO(ashankar): Hack. See comments in vsecurity.LoadIdentity.
	hack, err := isecurity.NewPrivateID("hack", nil)
	if err != nil {
		return nil, err
	}
	return vsecurity.LoadIdentity(f, hack)
}

func (rt *vrt) connectToAgent() (security.PrivateID, error) {
	// Verify we're communicating over unix domain sockets so
	// we know it's safe to use VCSecurityNone.
	endpoint, err := rt.NewEndpoint(os.Getenv(agent.EndpointVarName))
	if err != nil {
		return nil, err
	}
	if endpoint.Addr().Network() != unixfd.Network {
		return nil, verror.BadArgf("invalid agent address %v", endpoint.Addr())
	}

	client, err := rt.NewClient(veyron2.VCSecurityNone)
	if err != nil {
		return nil, err
	}
	signer, err := agent.NewAgentSigner(client, endpoint.String(), rt.NewContext())
	if err != nil {
		return nil, err
	}
	return isecurity.NewPrivateID("selfSigned", signer)
}

package rt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"os"
	"os/user"
	"path"
	"strconv"

	isecurity "veyron.io/veyron/veyron/runtimes/google/security"
	vsecurity "veyron.io/veyron/veyron/security"
	"veyron.io/veyron/veyron/security/agent"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

const (
	privateKeyFile = "privatekey.pem"
	// Environment variable pointing to a directory where information about a principal
	// (private key, blessing store, blessing roots etc.) is stored.
	VeyronCredentialsEnvVar = "VEYRON_CREDENTIALS"
)

func (rt *vrt) Principal() security.Principal {
	return rt.principal
}

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
	if err := rt.initOldSecurity(); err != nil {
		return err
	}
	if err := rt.initPrincipal(); err != nil {
		return fmt.Errorf("principal initialization failed: %v", err)
	}
	if err := rt.initDefaultBlessings(); err != nil {
		return fmt.Errorf("default blessing initialization failed: %v", err)
	}
	return nil
}

func (rt *vrt) initPrincipal() error {
	// TODO(ataly, ashankar): Check if agent environment variables are
	// specified and if so initialize principal from agent.
	if dir := os.Getenv(VeyronCredentialsEnvVar); len(dir) > 0 {
		// TODO(ataly, ashankar): If multiple runtimes are getting
		// initialized at the same time from the same VEYRON_CREDENTIALS
		// we will need some kind of locking for the credential files.
		return rt.initPrincipalFromCredentials(dir)
	}
	return rt.initTemporaryPrincipal()
}

func (rt *vrt) initDefaultBlessings() error {
	if rt.principal.BlessingStore().Default() != nil {
		return nil
	}
	blessing, err := rt.principal.BlessSelf(defaultBlessingName())
	if err != nil {
		return err
	}
	if err := rt.principal.BlessingStore().SetDefault(blessing); err != nil {
		return err
	}
	if _, err := rt.principal.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
		return err
	}
	if err := rt.principal.AddToRoots(blessing); err != nil {
		return err
	}
	return nil
}

func (rt *vrt) initPrincipalFromCredentials(dir string) error {
	if finfo, err := os.Stat(dir); err == nil {
		if !finfo.IsDir() {
			return fmt.Errorf("%q is not a directory", dir)
		}
	} else if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create %q: %v", dir, err)
	}
	key, err := initKey(dir)
	if err != nil {
		return fmt.Errorf("could not initialize private key from credentials directory %v: %v", dir, err)
	}

	signer := security.NewInMemoryECDSASigner(key)
	store, roots, err := initStoreAndRootsFromCredentials(dir, signer)
	if err != nil {
		return fmt.Errorf("could not initialize BlessingStore and BlessingRoots from credentials directory %v: %v", dir, err)
	}

	if rt.principal, err = security.CreatePrincipal(signer, store, roots); err != nil {
		return fmt.Errorf("could not create Principal object: %v", err)
	}
	return nil
}

func (rt *vrt) initTemporaryPrincipal() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	signer := security.NewInMemoryECDSASigner(key)
	if rt.principal, err = security.CreatePrincipal(signer, newInMemoryBlessingStore(signer.PublicKey()), newInMemoryBlessingRoots()); err != nil {
		return fmt.Errorf("could not create Principal object: %v", err)
	}
	return nil
}

// TODO(ataly, ashankar): Get rid of this method once we get rid of
// PrivateID and PublicIDStore.
func (rt *vrt) initOldSecurity() error {
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
	if len(os.Getenv(agent.FdVarName)) > 0 {
		rt.id, err = rt.connectToAgent()
		return err
	} else if file := os.Getenv("VEYRON_IDENTITY"); len(file) > 0 {
		if rt.id, err = loadIdentityFromFile(file); err != nil || rt.id == nil {
			return fmt.Errorf("Could not load identity from the VEYRON_IDENTITY environment variable (%q): %v", file, err)
		}
	} else {
		name := defaultBlessingName()
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
	client, err := rt.NewClient(veyron2.VCSecurityNone)
	if err != nil {
		return nil, err
	}
	fd, err := strconv.Atoi(os.Getenv(agent.FdVarName))
	if err != nil {
		return nil, err
	}
	signer, err := agent.NewAgentSigner(client, fd, rt.NewContext())
	if err != nil {
		return nil, err
	}
	return isecurity.NewPrivateID("selfSigned", signer)
}

func initKey(dir string) (*ecdsa.PrivateKey, error) {
	keyPath := path.Join(dir, privateKeyFile)
	if f, err := os.Open(keyPath); err == nil {
		defer f.Close()
		v, err := vsecurity.LoadPEMKey(f, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to load PEM data from %q: %v", keyPath, v)
		}
		key, ok := v.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%q contains a %T, not an ECDSA private key", keyPath, v)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read %q: %v", keyPath, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate a private key: %v", err)
	}

	f, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open %q for writing: %v", keyPath, err)
	}
	defer f.Close()
	return key, vsecurity.SavePEMKey(f, key, nil)
}

func initStoreAndRootsFromCredentials(dir string, secsigner security.Signer) (security.BlessingStore, security.BlessingRoots, error) {
	signer, err := security.CreatePrincipal(secsigner, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	store, err := newPersistingBlessingStore(signer.PublicKey(), dir, signer)
	if err != nil {
		return nil, nil, err
	}
	roots, err := newPersistingBlessingRoots(dir, signer)
	if err != nil {
		return nil, nil, err
	}
	return store, roots, nil
}

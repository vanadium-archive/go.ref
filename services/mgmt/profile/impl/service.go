package impl

import (
	"errors"

	"v.io/core/veyron/services/mgmt/lib/fs"
	"v.io/core/veyron/services/mgmt/profile"
	"v.io/core/veyron/services/mgmt/repository"

	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/vlog"
)

// profileService implements the Profile server interface.
type profileService struct {
	// store is the storage server used for storing profile data.
	store *fs.Memstore
	// storeRoot is a name in the Store under which all data will be stored.
	storeRoot string
	// suffix is the name of the profile specification.
	suffix string
}

var (
	errNotFound        = errors.New("not found")
	errOperationFailed = errors.New("operation failed")
)

// NewProfileService returns a new Profile service implementation.
func NewProfileService(store *fs.Memstore, storeRoot, suffix string) repository.ProfileServerMethods {
	return &profileService{store: store, storeRoot: storeRoot, suffix: suffix}
}

// STORE MANAGEMENT INTERFACE IMPLEMENTATION

func (i *profileService) Put(context ipc.ServerContext, profile profile.Specification) error {
	vlog.VI(0).Infof("%v.Put(%v)", i.suffix, profile)
	// Transaction is rooted at "", so tname == tid.
	i.store.Lock()
	defer i.store.Unlock()
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(context)
	if err != nil {
		return err
	}
	path := naming.Join(tname, "/profiles", i.suffix)
	object := i.store.BindObject(path)
	if _, err := object.Put(context, profile); err != nil {
		return errOperationFailed
	}
	if err := i.store.BindTransaction(tname).Commit(context); err != nil {
		return errOperationFailed
	}
	return nil
}

func (i *profileService) Remove(context ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Remove()", i.suffix)
	i.store.Lock()
	defer i.store.Unlock()
	// Transaction is rooted at "", so tname == tid.
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(context)
	if err != nil {
		return err
	}
	path := naming.Join(tname, "/profiles", i.suffix)
	object := i.store.BindObject(path)
	found, err := object.Exists(context)
	if err != nil {
		return errOperationFailed
	}
	if !found {
		return errNotFound
	}
	if err := object.Remove(context); err != nil {
		return errOperationFailed
	}
	if err := i.store.BindTransaction(tname).Commit(context); err != nil {
		return errOperationFailed
	}
	return nil
}

// PROFILE INTERACE IMPLEMENTATION

func (i *profileService) lookup(context ipc.ServerContext) (profile.Specification, error) {
	empty := profile.Specification{}
	path := naming.Join("/profiles", i.suffix)

	i.store.Lock()
	defer i.store.Unlock()

	entry, err := i.store.BindObject(path).Get(context)
	if err != nil {
		return empty, errNotFound
	}
	s, ok := entry.Value.(profile.Specification)
	if !ok {
		return empty, errOperationFailed
	}
	return s, nil
}

func (i *profileService) Label(context ipc.ServerContext) (string, error) {
	vlog.VI(0).Infof("%v.Label()", i.suffix)
	s, err := i.lookup(context)
	if err != nil {
		return "", err
	}
	return s.Label, nil
}

func (i *profileService) Description(context ipc.ServerContext) (string, error) {
	vlog.VI(0).Infof("%v.Description()", i.suffix)
	s, err := i.lookup(context)
	if err != nil {
		return "", err
	}
	return s.Description, nil
}

func (i *profileService) Specification(context ipc.ServerContext) (profile.Specification, error) {
	vlog.VI(0).Infof("%v.Specification()", i.suffix)
	return i.lookup(context)
}

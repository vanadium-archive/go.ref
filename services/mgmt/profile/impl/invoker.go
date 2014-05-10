package impl

import (
	"errors"
	"path"

	"veyron/services/mgmt/profile"

	"veyron2/ipc"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/vlog"
)

// invoker holds the profile manager invocation.
type invoker struct {
	// store is the storage server used for storing profile data.
	store storage.Store
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative veyron name to identify a profile
	// specification.
	suffix string
}

var (
	errNotFound        = errors.New("not found")
	errOperationFailed = errors.New("operation failed")
)

// NewInvoker is the invoker factory.
func NewInvoker(store storage.Store, suffix string) *invoker {
	return &invoker{store: store, suffix: suffix}
}

// STORE MANAGEMENT INTERFACE IMPLEMENTATION

// dir is used to organize directory contents in the store.
type dir struct{}

// makeParentNodes creates the parent nodes if they do not already exist.
func makeParentNodes(store storage.Store, transaction storage.Transaction, path string) error {
	pathComponents := storage.ParsePath(path)
	for i := 0; i < len(pathComponents); i++ {
		name := pathComponents[:i].String()
		object := store.Bind(name)
		if _, err := object.Get(transaction); err != nil {
			if _, err := object.Put(transaction, &dir{}); err != nil {
				return errOperationFailed
			}
		}
	}
	return nil
}

func (i *invoker) Put(context ipc.Context, profile profile.Specification) error {
	vlog.VI(0).Infof("%v.Put(%v)", i.suffix, profile)
	transaction := vstore.NewTransaction()
	path := path.Join("/profiles", i.suffix)
	if err := makeParentNodes(i.store, transaction, path); err != nil {
		return err
	}
	object := i.store.Bind(path)
	if _, err := object.Put(transaction, profile); err != nil {
		return errOperationFailed
	}
	if err := transaction.Commit(); err != nil {
		return errOperationFailed
	}
	return nil
}

func (i *invoker) Remove(context ipc.Context) error {
	vlog.VI(0).Infof("%v.Remove(%v)", i.suffix)
	transaction := vstore.NewTransaction()
	path := path.Join("/profiles", i.suffix)
	object := i.store.Bind(path)
	found, err := object.Exists(transaction)
	if err != nil {
		return errOperationFailed
	}
	if !found {
		return errNotFound
	}
	if err := object.Remove(transaction); err != nil {
		return errOperationFailed
	}
	if err := transaction.Commit(); err != nil {
		return errOperationFailed
	}
	return nil
}

// PROFILE INTERACE IMPLEMENTATION

func (i *invoker) lookup() (profile.Specification, error) {
	empty := profile.Specification{}
	path := path.Join("/profiles", i.suffix)
	object := i.store.Bind(path)
	entry, err := object.Get(nil)
	if err != nil {
		return empty, errNotFound
	}
	s, ok := entry.Value.(profile.Specification)
	if !ok {
		return empty, errOperationFailed
	}
	return s, nil
}

func (i *invoker) Label(context ipc.Context) (string, error) {
	vlog.VI(0).Infof("%v.Label()", i.suffix)
	s, err := i.lookup()
	if err != nil {
		return "", err
	}
	return s.Label, nil
}

func (i *invoker) Description(context ipc.Context) (string, error) {
	vlog.VI(0).Infof("%v.Description()", i.suffix)
	s, err := i.lookup()
	if err != nil {
		return "", err
	}
	return s.Description, nil
}

func (i *invoker) Specification(context ipc.Context) (profile.Specification, error) {
	vlog.VI(0).Infof("%v.Specification()", i.suffix)
	return i.lookup()
}

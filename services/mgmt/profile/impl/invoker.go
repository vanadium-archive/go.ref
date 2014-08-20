package impl

import (
	"errors"

	"veyron/services/mgmt/profile"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/vlog"
)

// invoker holds the profile repository invocation.
type invoker struct {
	// storeRoot is a name in the Store under which all data will be stored.
	storeRoot string
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative object name to identify a profile
	// specification.
	suffix string
}

var (
	errNotFound        = errors.New("not found")
	errOperationFailed = errors.New("operation failed")
)

// NewInvoker is the invoker factory.
func NewInvoker(storeRoot, suffix string) *invoker {
	return &invoker{storeRoot: storeRoot, suffix: suffix}
}

// STORE MANAGEMENT INTERFACE IMPLEMENTATION

// dir is used to organize directory contents in the store.
type dir struct{}

// makeParentNodes creates the parent nodes if they do not already exist.
func makeParentNodes(context ipc.ServerContext, tx storage.Transaction, path string) error {
	pathComponents := storage.ParsePath(path)
	for i := 0; i < len(pathComponents); i++ {
		name := pathComponents[:i].String()
		object := tx.Bind(name)
		if _, err := object.Get(context); err != nil {
			if _, err := object.Put(context, &dir{}); err != nil {
				return errOperationFailed
			}
		}
	}
	return nil
}

func (i *invoker) Put(context ipc.ServerContext, profile profile.Specification) error {
	vlog.VI(0).Infof("%v.Put(%v)", i.suffix, profile)
	tx := vstore.New().NewTransaction(context, i.storeRoot)
	path := naming.Join("/profiles", i.suffix)
	if err := makeParentNodes(context, tx, path); err != nil {
		return err
	}
	object := tx.Bind(path)
	if _, err := object.Put(context, profile); err != nil {
		return errOperationFailed
	}
	if err := tx.Commit(context); err != nil {
		return errOperationFailed
	}
	return nil
}

func (i *invoker) Remove(context ipc.ServerContext) error {
	vlog.VI(0).Infof("%v.Remove()", i.suffix)
	tx := vstore.New().NewTransaction(context, i.storeRoot)
	path := naming.Join("/profiles", i.suffix)
	object := tx.Bind(path)
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
	if err := tx.Commit(context); err != nil {
		return errOperationFailed
	}
	return nil
}

// PROFILE INTERACE IMPLEMENTATION

func (i *invoker) lookup(context ipc.ServerContext) (profile.Specification, error) {
	empty := profile.Specification{}
	path := naming.Join(i.storeRoot, "/profiles", i.suffix)
	entry, err := vstore.New().Bind(path).Get(context)
	if err != nil {
		return empty, errNotFound
	}
	s, ok := entry.Value.(profile.Specification)
	if !ok {
		return empty, errOperationFailed
	}
	return s, nil
}

func (i *invoker) Label(context ipc.ServerContext) (string, error) {
	vlog.VI(0).Infof("%v.Label()", i.suffix)
	s, err := i.lookup(context)
	if err != nil {
		return "", err
	}
	return s.Label, nil
}

func (i *invoker) Description(context ipc.ServerContext) (string, error) {
	vlog.VI(0).Infof("%v.Description()", i.suffix)
	s, err := i.lookup(context)
	if err != nil {
		return "", err
	}
	return s.Description, nil
}

func (i *invoker) Specification(context ipc.ServerContext) (profile.Specification, error) {
	vlog.VI(0).Infof("%v.Specification()", i.suffix)
	return i.lookup(context)
}

package impl

import (
	"errors"
	"path"
	"strings"

	"veyron2/ipc"
	"veyron2/services/mgmt/application"
	"veyron2/storage"
	"veyron2/storage/vstore/primitives"
	"veyron2/vlog"
)

// invoker holds the state of an application manager invocation.
type invoker struct {
	// store is the storage server used for storing application
	// metadata.
	store storage.Store
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative veyron name to identify an application.
	suffix string
}

var (
	errInvalidSuffix   = errors.New("invalid suffix")
	errOperationFailed = errors.New("operation failed")
	errNotFound        = errors.New("not found")
)

// NewInvoker is the invoker factory.
func NewInvoker(store storage.Store, suffix string) *invoker {
	return &invoker{store: store, suffix: suffix}
}

func parse(suffix string) (string, string, error) {
	tokens := strings.Split(suffix, "/")
	switch len(tokens) {
	case 2:
		return tokens[0], tokens[1], nil
	case 1:
		return tokens[0], "", nil
	default:
		return "", "", errInvalidSuffix
	}
}

// APPLICATION INTERFACE IMPLEMENTATION

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

func (i *invoker) Match(context ipc.Context, profiles []string) (application.Envelope, error) {
	vlog.VI(0).Infof("%v.Match(%v)", i.suffix, profiles)
	empty := application.Envelope{}
	name, version, err := parse(i.suffix)
	if err != nil {
		return empty, err
	}
	if version == "" {
		return empty, errInvalidSuffix
	}
	for _, profile := range profiles {
		path := path.Join("/applications", name, profile, version)
		object := i.store.Bind(path)
		entry, err := object.Get(nil)
		if err != nil {
			continue
		}
		envelope, ok := entry.Value.(application.Envelope)
		if !ok {
			continue
		}
		return envelope, nil
	}
	return empty, errNotFound
}

func (i *invoker) Put(context ipc.Context, profiles []string, envelope application.Envelope) error {
	vlog.VI(0).Infof("%v.Put(%v, %v)", i.suffix, profiles, envelope)
	name, version, err := parse(i.suffix)
	if err != nil {
		return err
	}
	if version == "" {
		return errInvalidSuffix
	}
	transaction := primitives.NewTransaction()
	var entry storage.Stat
	for _, profile := range profiles {
		path := path.Join("/applications", name, profile, version)
		if err := makeParentNodes(i.store, transaction, path); err != nil {
			return err
		}
		object := i.store.Bind(path)
		if !entry.ID.IsValid() {
			if entry, err = object.Put(transaction, envelope); err != nil {
				return errOperationFailed
			}
		} else {
			if _, err := object.Put(transaction, entry.ID); err != nil {
				return errOperationFailed
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return errOperationFailed
	}
	return nil
}

func (i *invoker) Remove(context ipc.Context, profile string) error {
	vlog.VI(0).Infof("%v.Remove(%v)", i.suffix, profile)
	name, version, err := parse(i.suffix)
	if err != nil {
		return err
	}
	transaction := primitives.NewTransaction()
	path := path.Join("/applications", name, profile)
	if version != "" {
		path += "/" + version
	}
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

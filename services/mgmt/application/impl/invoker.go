package impl

import (
	"errors"
	"path"
	"strings"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/services/mgmt/application"
	"veyron2/storage"
	"veyron2/vlog"
)

// invoker holds the state of an application repository invocation.
type invoker struct {
	// store is the storage server used for storing application
	// metadata.
	store storage.Store
	// suffix is the suffix of the current invocation that is assumed to
	// be used as a relative object name to identify an application.
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
func makeParentNodes(context ipc.ServerContext, store storage.Store, path string) error {
	pathComponents := storage.ParsePath(path)
	for i := 0; i < len(pathComponents); i++ {
		name := pathComponents[:i].String()
		object := store.BindObject(name)
		if _, err := object.Get(context); err != nil {
			if _, err := object.Put(context, &dir{}); err != nil {
				return errOperationFailed
			}
		}
	}
	return nil
}

func (i *invoker) Match(context ipc.ServerContext, profiles []string) (application.Envelope, error) {
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
		entry, err := i.store.BindObject(path).Get(context)
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

func (i *invoker) Put(context ipc.ServerContext, profiles []string, envelope application.Envelope) error {
	vlog.VI(0).Infof("%v.Put(%v, %v)", i.suffix, profiles, envelope)
	name, version, err := parse(i.suffix)
	if err != nil {
		return err
	}
	if version == "" {
		return errInvalidSuffix
	}
	// Transaction is rooted at "", so tname == tid.
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(context)
	if err != nil {
		return err
	}
	var entry storage.Stat
	for _, profile := range profiles {
		path := naming.Join(tname, path.Join("/applications", name, profile, version))
		if err := makeParentNodes(context, i.store, path); err != nil {
			return err
		}
		object := i.store.BindObject(path)
		if !entry.ID.IsValid() {
			if entry, err = object.Put(context, envelope); err != nil {
				return errOperationFailed
			}
		} else {
			if _, err := object.Put(context, entry.ID); err != nil {
				return errOperationFailed
			}
		}
	}
	if err := i.store.BindTransaction(tname).Commit(context); err != nil {
		return errOperationFailed
	}
	return nil
}

func (i *invoker) Remove(context ipc.ServerContext, profile string) error {
	vlog.VI(0).Infof("%v.Remove(%v)", i.suffix, profile)
	name, version, err := parse(i.suffix)
	if err != nil {
		return err
	}
	// Transaction is rooted at "", so tname == tid.
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(context)
	if err != nil {
		return err
	}
	path := naming.Join(tname, path.Join("/applications", name, profile))
	if version != "" {
		path += "/" + version
	}
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

package impl

import (
	"errors"
	"strings"

	_ "veyron/services/store/typeregistryhack"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/services/mgmt/application"
	"veyron2/storage"
	"veyron2/storage/vstore"
	"veyron2/vlog"
)

// invoker holds the state of an application repository invocation.
type invoker struct {
	// storeRoot is a name in the Store under which all data will be stored.
	storeRoot string
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
func NewInvoker(storeRoot, suffix string) *invoker {
	return &invoker{storeRoot: storeRoot, suffix: suffix}
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
func makeParentNodes(context ipc.ServerContext, tx storage.Transaction, path string) error {
	pathComponents := storage.ParsePath(path)
	for i := 0; i < len(pathComponents); i++ {
		name := pathComponents[:i].String()
		object := tx.Bind(name)
		if exists, err := object.Exists(context); err != nil {
			return errOperationFailed
		} else if !exists {
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
	root := vstore.New().Bind(i.storeRoot)
	for _, profile := range profiles {
		path := naming.Join("/applications", name, profile, version)
		entry, err := root.Bind(path).Get(context)
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
	tx := vstore.New().NewTransaction(context, i.storeRoot)
	var entry storage.Stat
	for _, profile := range profiles {
		path := naming.Join("/applications", name, profile, version)
		if err := makeParentNodes(context, tx, path); err != nil {
			return err
		}
		object := tx.Bind(path)
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
	if err := tx.Commit(context); err != nil {
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
	tx := vstore.New().NewTransaction(context, i.storeRoot)
	path := naming.Join("/applications", name, profile, version)
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

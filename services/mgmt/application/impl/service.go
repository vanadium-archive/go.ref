// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"strings"

	"v.io/x/ref/services/mgmt/lib/acls"
	"v.io/x/ref/services/mgmt/lib/fs"
	"v.io/x/ref/services/mgmt/repository"

	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/services/mgmt/application"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

// appRepoService implements the Application repository interface.
type appRepoService struct {
	// store is the storage server used for storing application
	// metadata.
	// All objects share the same Memstore.
	store *fs.Memstore
	// storeRoot is a name in the directory under which all data will be
	// stored.
	storeRoot string
	// suffix is the name of the application object.
	suffix string
}

const pkgPath = "v.io/x/ref/services/mgmt/application/impl/"

var (
	ErrInvalidSuffix   = verror.Register(pkgPath+".InvalidSuffix", verror.NoRetry, "{1:}{2:} invalid suffix{:_}")
	ErrOperationFailed = verror.Register(pkgPath+".OperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
	ErrNotFound        = verror.Register(pkgPath+".NotFound", verror.NoRetry, "{1:}{2:} not found{:_}")
	ErrInvalidBlessing = verror.Register(pkgPath+".InvalidBlessing", verror.NoRetry, "{1:}{2:} invalid blessing{:_}")
)

// NewApplicationService returns a new Application service implementation.
func NewApplicationService(store *fs.Memstore, storeRoot, suffix string) repository.ApplicationServerMethods {
	return &appRepoService{store: store, storeRoot: storeRoot, suffix: suffix}
}

func parse(call rpc.ServerCall, suffix string) (string, string, error) {
	tokens := strings.Split(suffix, "/")
	switch len(tokens) {
	case 2:
		return tokens[0], tokens[1], nil
	case 1:
		return tokens[0], "", nil
	default:
		return "", "", verror.New(ErrInvalidSuffix, call.Context())
	}
}

func (i *appRepoService) Match(call rpc.ServerCall, profiles []string) (application.Envelope, error) {
	vlog.VI(0).Infof("%v.Match(%v)", i.suffix, profiles)
	empty := application.Envelope{}
	name, version, err := parse(call, i.suffix)
	if err != nil {
		return empty, err
	}
	if version == "" {
		return empty, verror.New(ErrInvalidSuffix, call.Context())
	}

	i.store.Lock()
	defer i.store.Unlock()

	for _, profile := range profiles {
		path := naming.Join("/applications", name, profile, version)
		entry, err := i.store.BindObject(path).Get(call)
		if err != nil {
			continue
		}
		envelope, ok := entry.Value.(application.Envelope)
		if !ok {
			continue
		}
		return envelope, nil
	}
	return empty, verror.New(ErrNotFound, call.Context())
}

func (i *appRepoService) Put(call rpc.ServerCall, profiles []string, envelope application.Envelope) error {
	vlog.VI(0).Infof("%v.Put(%v, %v)", i.suffix, profiles, envelope)
	name, version, err := parse(call, i.suffix)
	if err != nil {
		return err
	}
	if version == "" {
		return verror.New(ErrInvalidSuffix, call.Context())
	}
	i.store.Lock()
	defer i.store.Unlock()
	// Transaction is rooted at "", so tname == tid.
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(call)
	if err != nil {
		return err
	}

	for _, profile := range profiles {
		path := naming.Join(tname, "/applications", name, profile, version)

		object := i.store.BindObject(path)
		_, err := object.Put(call, envelope)
		if err != nil {
			return verror.New(ErrOperationFailed, call.Context())
		}
	}
	if err := i.store.BindTransaction(tname).Commit(call); err != nil {
		return verror.New(ErrOperationFailed, call.Context())
	}
	return nil
}

func (i *appRepoService) Remove(call rpc.ServerCall, profile string) error {
	vlog.VI(0).Infof("%v.Remove(%v)", i.suffix, profile)
	name, version, err := parse(call, i.suffix)
	if err != nil {
		return err
	}
	i.store.Lock()
	defer i.store.Unlock()
	// Transaction is rooted at "", so tname == tid.
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(call)
	if err != nil {
		return err
	}
	path := naming.Join(tname, "/applications", name, profile)
	if version != "" {
		path += "/" + version
	}
	object := i.store.BindObject(path)
	found, err := object.Exists(call)
	if err != nil {
		return verror.New(ErrOperationFailed, call.Context())
	}
	if !found {
		return verror.New(ErrNotFound, call.Context())
	}
	if err := object.Remove(call); err != nil {
		return verror.New(ErrOperationFailed, call.Context())
	}
	if err := i.store.BindTransaction(tname).Commit(call); err != nil {
		return verror.New(ErrOperationFailed, call.Context())
	}
	return nil
}

func (i *appRepoService) allApplications() ([]string, error) {
	apps, err := i.store.BindObject("/applications").Children()
	if err != nil {
		return nil, err
	}
	return apps, nil
}

func (i *appRepoService) allAppVersions(appName string) ([]string, error) {
	profiles, err := i.store.BindObject(naming.Join("/applications", appName)).Children()
	if err != nil {
		return nil, err
	}
	uniqueVersions := make(map[string]struct{})
	for _, profile := range profiles {
		versions, err := i.store.BindObject(naming.Join("/applications", appName, profile)).Children()
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			uniqueVersions[v] = struct{}{}
		}
	}
	versions := make([]string, len(uniqueVersions))
	index := 0
	for v, _ := range uniqueVersions {
		versions[index] = v
		index++
	}
	return versions, nil
}

func (i *appRepoService) GlobChildren__(rpc.ServerCall) (<-chan string, error) {
	vlog.VI(0).Infof("%v.GlobChildren__()", i.suffix)
	i.store.Lock()
	defer i.store.Unlock()

	var elems []string
	if i.suffix != "" {
		elems = strings.Split(i.suffix, "/")
	}

	var results []string
	var err error
	switch len(elems) {
	case 0:
		results, err = i.allApplications()
		if err != nil {
			return nil, err
		}
	case 1:
		results, err = i.allAppVersions(elems[0])
		if err != nil {
			return nil, err
		}
	case 2:
		versions, err := i.allAppVersions(elems[0])
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			if v == elems[1] {
				return nil, nil
			}
		}
		return nil, verror.New(ErrNotFound, nil)
	default:
		return nil, verror.New(ErrNotFound, nil)
	}

	ch := make(chan string, len(results))
	for _, r := range results {
		ch <- r
	}
	close(ch)
	return ch, nil
}

func (i *appRepoService) GetPermissions(call rpc.ServerCall) (acl access.Permissions, etag string, err error) {
	i.store.Lock()
	defer i.store.Unlock()
	path := naming.Join("/acls", i.suffix, "data")
	return getAccessList(i.store, path)
}

func (i *appRepoService) SetPermissions(call rpc.ServerCall, acl access.Permissions, etag string) error {
	i.store.Lock()
	defer i.store.Unlock()
	path := naming.Join("/acls", i.suffix, "data")
	return setAccessList(i.store, path, acl, etag)
}

// getAccessList fetches a Permissions out of the Memstore at the provided path.
// path is expected to already have been cleaned by naming.Join or its ilk.
func getAccessList(store *fs.Memstore, path string) (access.Permissions, string, error) {
	entry, err := store.BindObject(path).Get(nil)

	if verror.ErrorID(err) == fs.ErrNotInMemStore.ID {
		// No AccessList exists
		return nil, "", verror.New(ErrNotFound, nil)
	} else if err != nil {
		vlog.Errorf("getAccessList: internal failure in fs.Memstore")
		return nil, "", err
	}

	acl, ok := entry.Value.(access.Permissions)
	if !ok {
		return nil, "", err
	}

	etag, err := acls.ComputeEtag(acl)
	if err != nil {
		return nil, "", err
	}
	return acl, etag, nil
}

// setAccessList writes a Permissions into the Memstore at the provided path.
// where path is expected to have already been cleaned by naming.Join.
func setAccessList(store *fs.Memstore, path string, acl access.Permissions, etag string) error {
	_, oetag, err := getAccessList(store, path)
	if verror.ErrorID(err) == ErrNotFound.ID {
		oetag = etag
	} else if err != nil {
		return err
	}

	if oetag != etag {
		return verror.NewErrBadEtag(nil)
	}

	tname, err := store.BindTransactionRoot("").CreateTransaction(nil)
	if err != nil {
		return err
	}

	object := store.BindObject(path)

	if _, err := object.Put(nil, acl); err != nil {
		return err
	}
	if err := store.BindTransaction(tname).Commit(nil); err != nil {
		return verror.New(ErrOperationFailed, nil)
	}
	return nil
}

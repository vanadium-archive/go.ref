// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"strings"

	"v.io/x/ref/services/internal/acls"
	"v.io/x/ref/services/internal/fs"
	"v.io/x/ref/services/repository"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/application"
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

const pkgPath = "v.io/x/ref/services/application/applicationd/"

var (
	ErrInvalidSuffix   = verror.Register(pkgPath+".InvalidSuffix", verror.NoRetry, "{1:}{2:} invalid suffix{:_}")
	ErrOperationFailed = verror.Register(pkgPath+".OperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
	ErrNotAuthorized   = verror.Register(pkgPath+".errNotAuthorized", verror.NoRetry, "{1:}{2:} none of the client's blessings are valid {:_}")
)

// NewApplicationService returns a new Application service implementation.
func NewApplicationService(store *fs.Memstore, storeRoot, suffix string) repository.ApplicationServerMethods {
	return &appRepoService{store: store, storeRoot: storeRoot, suffix: suffix}
}

func parse(ctx *context.T, suffix string) (string, string, error) {
	tokens := strings.Split(suffix, "/")
	switch len(tokens) {
	case 2:
		return tokens[0], tokens[1], nil
	case 1:
		return tokens[0], "", nil
	default:
		return "", "", verror.New(ErrInvalidSuffix, ctx)
	}
}

func (i *appRepoService) Match(ctx *context.T, call rpc.ServerCall, profiles []string) (application.Envelope, error) {
	vlog.VI(0).Infof("%v.Match(%v)", i.suffix, profiles)
	empty := application.Envelope{}
	name, version, err := parse(ctx, i.suffix)
	if err != nil {
		return empty, err
	}
	if version == "" {
		return empty, verror.New(ErrInvalidSuffix, ctx)
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
	return empty, verror.New(verror.ErrNoExist, ctx)
}

func (i *appRepoService) Put(ctx *context.T, call rpc.ServerCall, profiles []string, envelope application.Envelope) error {
	vlog.VI(0).Infof("%v.Put(%v, %v)", i.suffix, profiles, envelope)
	name, version, err := parse(ctx, i.suffix)
	if err != nil {
		return err
	}
	if version == "" {
		return verror.New(ErrInvalidSuffix, ctx)
	}
	i.store.Lock()
	defer i.store.Unlock()
	// Transaction is rooted at "", so tname == tid.
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(call)
	if err != nil {
		return err
	}

	// Only add a Permission list value if there is not already one
	// present.
	apath := naming.Join("/acls", name, "data")
	aobj := i.store.BindObject(apath)
	if _, err := aobj.Get(call); verror.ErrorID(err) == fs.ErrNotInMemStore.ID {
		rb, _ := security.RemoteBlessingNames(ctx)
		if len(rb) == 0 {
			// None of the client's blessings are valid.
			return verror.New(ErrNotAuthorized, ctx)
		}
		newacls := acls.PermissionsForBlessings(rb)
		if _, err := aobj.Put(nil, newacls); err != nil {
			return err
		}
	}

	for _, profile := range profiles {
		path := naming.Join(tname, "/applications", name, profile, version)

		object := i.store.BindObject(path)
		_, err := object.Put(call, envelope)
		if err != nil {
			return verror.New(ErrOperationFailed, ctx)
		}
	}
	if err := i.store.BindTransaction(tname).Commit(call); err != nil {
		return verror.New(ErrOperationFailed, ctx)
	}
	return nil
}

func (i *appRepoService) Remove(ctx *context.T, call rpc.ServerCall, profile string) error {
	vlog.VI(0).Infof("%v.Remove(%v)", i.suffix, profile)
	name, version, err := parse(ctx, i.suffix)
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
		return verror.New(ErrOperationFailed, ctx)
	}
	if !found {
		return verror.New(verror.ErrNoExist, ctx)
	}
	if err := object.Remove(call); err != nil {
		return verror.New(ErrOperationFailed, ctx)
	}
	if err := i.store.BindTransaction(tname).Commit(call); err != nil {
		return verror.New(ErrOperationFailed, ctx)
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

func (i *appRepoService) GlobChildren__(*context.T, rpc.ServerCall) (<-chan string, error) {
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
		return nil, verror.New(verror.ErrNoExist, nil)
	default:
		return nil, verror.New(verror.ErrNoExist, nil)
	}

	ch := make(chan string, len(results))
	for _, r := range results {
		ch <- r
	}
	close(ch)
	return ch, nil
}

func (i *appRepoService) GetPermissions(ctx *context.T, call rpc.ServerCall) (acl access.Permissions, version string, err error) {
	name, _, err := parse(ctx, i.suffix)
	if err != nil {
		return nil, "", err
	}
	i.store.Lock()
	defer i.store.Unlock()
	path := naming.Join("/acls", name, "data")

	acl, version, err = getAccessList(i.store, path)
	if verror.ErrorID(err) == verror.ErrNoExist.ID {
		return acls.NilAuthPermissions(ctx), "", nil
	}

	return acl, version, err
}

func (i *appRepoService) SetPermissions(ctx *context.T, _ rpc.ServerCall, acl access.Permissions, version string) error {
	name, _, err := parse(ctx, i.suffix)
	if err != nil {
		return err
	}
	i.store.Lock()
	defer i.store.Unlock()
	path := naming.Join("/acls", name, "data")
	return setAccessList(i.store, path, acl, version)
}

// getAccessList fetches a Permissions out of the Memstore at the provided path.
// path is expected to already have been cleaned by naming.Join or its ilk.
func getAccessList(store *fs.Memstore, path string) (access.Permissions, string, error) {
	entry, err := store.BindObject(path).Get(nil)

	if verror.ErrorID(err) == fs.ErrNotInMemStore.ID {
		// No AccessList exists
		return nil, "", verror.New(verror.ErrNoExist, nil)
	} else if err != nil {
		vlog.Errorf("getAccessList: internal failure in fs.Memstore")
		return nil, "", err
	}

	acl, ok := entry.Value.(access.Permissions)
	if !ok {
		return nil, "", err
	}

	version, err := acls.ComputeVersion(acl)
	if err != nil {
		return nil, "", err
	}
	return acl, version, nil
}

// setAccessList writes a Permissions into the Memstore at the provided path.
// where path is expected to have already been cleaned by naming.Join.
func setAccessList(store *fs.Memstore, path string, acl access.Permissions, version string) error {
	if version != "" {
		_, oversion, err := getAccessList(store, path)
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			oversion = version
		} else if err != nil {
			return err
		}

		if oversion != version {
			return verror.NewErrBadVersion(nil)
		}
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

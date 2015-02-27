package impl

import (
	"strings"

	"v.io/core/veyron/services/mgmt/lib/acls"
	"v.io/core/veyron/services/mgmt/lib/fs"
	"v.io/core/veyron/services/mgmt/repository"

	"v.io/v23/ipc"
	"v.io/v23/naming"
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

const pkgPath = "v.io/core/veyron/services/mgmt/application/impl/"

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

func parse(context ipc.ServerContext, suffix string) (string, string, error) {
	tokens := strings.Split(suffix, "/")
	switch len(tokens) {
	case 2:
		return tokens[0], tokens[1], nil
	case 1:
		return tokens[0], "", nil
	default:
		return "", "", verror.New(ErrInvalidSuffix, context.Context())
	}
}

func (i *appRepoService) Match(context ipc.ServerContext, profiles []string) (application.Envelope, error) {
	vlog.VI(0).Infof("%v.Match(%v)", i.suffix, profiles)
	empty := application.Envelope{}
	name, version, err := parse(context, i.suffix)
	if err != nil {
		return empty, err
	}
	if version == "" {
		return empty, verror.New(ErrInvalidSuffix, context.Context())
	}

	i.store.Lock()
	defer i.store.Unlock()

	for _, profile := range profiles {
		path := naming.Join("/applications", name, profile, version)
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
	return empty, verror.New(ErrNotFound, context.Context())
}

func (i *appRepoService) Put(context ipc.ServerContext, profiles []string, envelope application.Envelope) error {
	vlog.VI(0).Infof("%v.Put(%v, %v)", i.suffix, profiles, envelope)
	name, version, err := parse(context, i.suffix)
	if err != nil {
		return err
	}
	if version == "" {
		return verror.New(ErrInvalidSuffix, context.Context())
	}
	i.store.Lock()
	defer i.store.Unlock()
	// Transaction is rooted at "", so tname == tid.
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(context)
	if err != nil {
		return err
	}

	for _, profile := range profiles {
		path := naming.Join(tname, "/applications", name, profile, version)

		object := i.store.BindObject(path)
		_, err := object.Put(context, envelope)
		if err != nil {
			return verror.New(ErrOperationFailed, context.Context())
		}
	}
	if err := i.store.BindTransaction(tname).Commit(context); err != nil {
		return verror.New(ErrOperationFailed, context.Context())
	}
	return nil
}

func (i *appRepoService) Remove(context ipc.ServerContext, profile string) error {
	vlog.VI(0).Infof("%v.Remove(%v)", i.suffix, profile)
	name, version, err := parse(context, i.suffix)
	if err != nil {
		return err
	}
	i.store.Lock()
	defer i.store.Unlock()
	// Transaction is rooted at "", so tname == tid.
	tname, err := i.store.BindTransactionRoot("").CreateTransaction(context)
	if err != nil {
		return err
	}
	path := naming.Join(tname, "/applications", name, profile)
	if version != "" {
		path += "/" + version
	}
	object := i.store.BindObject(path)
	found, err := object.Exists(context)
	if err != nil {
		return verror.New(ErrOperationFailed, context.Context())
	}
	if !found {
		return verror.New(ErrNotFound, context.Context())
	}
	if err := object.Remove(context); err != nil {
		return verror.New(ErrOperationFailed, context.Context())
	}
	if err := i.store.BindTransaction(tname).Commit(context); err != nil {
		return verror.New(ErrOperationFailed, context.Context())
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

func (i *appRepoService) GlobChildren__(ipc.ServerContext) (<-chan string, error) {
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

func (i *appRepoService) GetACL(ctx ipc.ServerContext) (acl access.TaggedACLMap, etag string, err error) {
	i.store.Lock()
	defer i.store.Unlock()
	path := naming.Join("/acls", i.suffix, "data")
	return getACL(i.store, path)
}

func (i *appRepoService) SetACL(ctx ipc.ServerContext, acl access.TaggedACLMap, etag string) error {
	i.store.Lock()
	defer i.store.Unlock()
	path := naming.Join("/acls", i.suffix, "data")
	return setACL(i.store, path, acl, etag)
}

// getACL fetches a TaggedACLMap out of the Memstore at the provided path.
// path is expected to already have been cleaned by naming.Join or its ilk.
func getACL(store *fs.Memstore, path string) (access.TaggedACLMap, string, error) {
	entry, err := store.BindObject(path).Get(nil)

	if verror.Is(err, fs.ErrNotInMemStore.ID) {
		// No ACL exists
		return nil, "", verror.New(ErrNotFound, nil)
	} else if err != nil {
		vlog.Errorf("getACL: internal failure in fs.Memstore")
		return nil, "", err
	}

	acl, ok := entry.Value.(access.TaggedACLMap)
	if !ok {
		return nil, "", err
	}

	etag, err := acls.ComputeEtag(acl)
	if err != nil {
		return nil, "", err
	}
	return acl, etag, nil
}

// setACL writes a TaggedACLMap into the Memstore at the provided path.
// where path is expected to have already been cleaned by naming.Join.
func setACL(store *fs.Memstore, path string, acl access.TaggedACLMap, etag string) error {
	_, oetag, err := getACL(store, path)
	if verror.Is(err, ErrNotFound.ID) {
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

package impl

import (
	"strings"

	"veyron.io/veyron/veyron/services/mgmt/lib/fs"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"
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

const pkgPath = "veyron.io/veyron/veyron/services/mgmt/application/impl/"

var (
	errInvalidSuffix   = verror2.Register(pkgPath+".invalidSuffix", verror2.NoRetry, "")
	errOperationFailed = verror2.Register(pkgPath+".operationFailed", verror2.NoRetry, "")
	errNotFound        = verror2.Register(pkgPath+".notFound", verror2.NoRetry, "")
)

// NewApplicationService returns a new Application service implementation.
func NewApplicationService(store *fs.Memstore, storeRoot, suffix string) *appRepoService {
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
		return "", "", verror2.Make(errInvalidSuffix, context.Context())
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
		return empty, verror2.Make(errInvalidSuffix, context.Context())
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
	return empty, verror2.Make(errNotFound, context.Context())
}

func (i *appRepoService) Put(context ipc.ServerContext, profiles []string, envelope application.Envelope) error {
	vlog.VI(0).Infof("%v.Put(%v, %v)", i.suffix, profiles, envelope)
	name, version, err := parse(context, i.suffix)
	if err != nil {
		return err
	}
	if version == "" {
		return verror2.Make(errInvalidSuffix, context.Context())
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
			return verror2.Make(errOperationFailed, context.Context())
		}
	}
	if err := i.store.BindTransaction(tname).Commit(context); err != nil {
		return verror2.Make(errOperationFailed, context.Context())
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
		return verror2.Make(errOperationFailed, context.Context())
	}
	if !found {
		return verror2.Make(errNotFound, context.Context())
	}
	if err := object.Remove(context); err != nil {
		return verror2.Make(errOperationFailed, context.Context())
	}
	if err := i.store.BindTransaction(tname).Commit(context); err != nil {
		return verror2.Make(errOperationFailed, context.Context())
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
		return nil, verror2.Make(errNotFound, nil)
	default:
		return nil, verror2.Make(errNotFound, nil)
	}

	ch := make(chan string, len(results))
	for _, r := range results {
		ch <- r
	}
	close(ch)
	return ch, nil
}

package mounttable

import (
	"sync"

	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"
)

// collectionServer is a very simple collection server implementation for testing, with sufficient debugging to help
// when there are problems.
type collectionServer struct {
	sync.Mutex
	contents map[string][]byte
}
type collectionDispatcher struct {
	*collectionServer
}
type rpcContext struct {
	name string
	*collectionServer
}

var instance collectionServer

func newCollectionServer() *collectionDispatcher {
	return &collectionDispatcher{collectionServer: &collectionServer{contents: make(map[string][]byte)}}
}

// Lookup implements ipc.Dispatcher.Lookup.
func (d *collectionDispatcher) Lookup(name string) (interface{}, security.Authorizer, error) {
	rpcc := &rpcContext{name: name, collectionServer: d.collectionServer}
	return rpcc, d, nil
}

func (collectionDispatcher) Authorize(security.Context) error {
	return nil
}

// Export implements CollectionServerMethods.Export.
func (c *rpcContext) Export(ctx ipc.ServerContext, val []byte, overwrite bool) error {
	c.Lock()
	defer c.Unlock()
	if b := c.contents[c.name]; overwrite || b == nil {
		c.contents[c.name] = val
		return nil
	}
	return verror.New(naming.ErrNameExists, ctx.Context(), c.name)
}

// Lookup implements CollectionServerMethods.Lookup.
func (c *rpcContext) Lookup(ctx ipc.ServerContext) ([]byte, error) {
	c.Lock()
	defer c.Unlock()
	if val := c.contents[c.name]; val != nil {
		return val, nil
	}
	return nil, verror.New(naming.ErrNoSuchName, ctx.Context(), c.name)
}

package mounttable

import (
	"sync"

	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"
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
func (d *collectionDispatcher) Lookup(name string) (ipc.Invoker, security.Authorizer, error) {
	rpcc := &rpcContext{name: name, collectionServer: d.collectionServer}
	return ipc.ReflectInvoker(rpcc), d, nil
}

func (collectionDispatcher) Authorize(security.Context) error {
	return nil
}

// Export implements CollectionService.Export.
func (c *rpcContext) Export(_ ipc.ServerCall, val []byte, overwrite bool) error {
	c.Lock()
	defer c.Unlock()
	if b := c.contents[c.name]; overwrite || b == nil {
		c.contents[c.name] = val
		return nil
	}
	return naming.ErrNameExists
}

// Lookup implements CollectionService.Lookup.
func (c *rpcContext) Lookup(_ ipc.ServerCall) ([]byte, error) {
	c.Lock()
	defer c.Unlock()
	if val := c.contents[c.name]; val != nil {
		return val, nil
	}
	return nil, naming.ErrNoSuchName
}

// This file was auto-generated by the veyron vdl tool.
// Source: service.vdl

/*
Package raw defines a raw interface for the Veyron store.
The raw interface supports synchronizing with remote stores by transporting Mutations.
*/
package raw

import (
	"veyron2/services/watch"

	"veyron2/storage"

	// The non-user imports are prefixed with "_gen_" to prevent collisions.
	_gen_veyron2 "veyron2"
	_gen_context "veyron2/context"
	_gen_ipc "veyron2/ipc"
	_gen_naming "veyron2/naming"
	_gen_rt "veyron2/rt"
	_gen_vdl "veyron2/vdl"
	_gen_wiretype "veyron2/wiretype"
)

// Mutation represents an update to an entry in the store, and contains enough
// information for a privileged service to replicate the update elsewhere.
type Mutation struct {
	// ID is the key that identifies the entry.
	ID storage.ID
	// The version of the entry immediately before the update. For new entries,
	// the PriorVersion is NoVersion.
	PriorVersion storage.Version
	// The version of the entry immediately after the update. For deleted entries,
	// the Version is NoVersion.
	Version storage.Version
	// IsRoot is true if
	// 1) The entry was the store root immediately before being deleted, or
	// 2) The entry is the store root immediately after the update.
	IsRoot bool
	// Value is value stored at this entry.
	Value _gen_vdl.Any
	// Tags specify permissions on this entry.
	Tags storage.TagList
	// Dir is the implicit directory of this entry, and may contain references
	// to other entries in the store.
	Dir []storage.DEntry
}

const (
	// The raw Store has Veyron name "<mount>/.store.raw", where <mount> is the
	// Veyron name of the mount point.
	RawStoreSuffix = ".store.raw"
)

// Store defines a raw interface for the Veyron store. Mutations can be received
// via the Watcher interface, and committed via PutMutation.
// Store is the interface the client binds and uses.
// Store_ExcludingUniversal is the interface without internal framework-added methods
// to enable embedding without method collisions.  Not to be used directly by clients.
type Store_ExcludingUniversal interface {
	// Watcher allows a client to receive updates for changes to objects
	// that match a query.  See the package comments for details.
	watch.Watcher_ExcludingUniversal
	// PutMutations atomically commits a stream of Mutations when the stream is
	// closed. Mutations are not committed if the request is cancelled before
	// the stream has been closed.
	PutMutations(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply StorePutMutationsStream, err error)
}
type Store interface {
	_gen_ipc.UniversalServiceMethods
	Store_ExcludingUniversal
}

// StoreService is the interface the server implements.
type StoreService interface {

	// Watcher allows a client to receive updates for changes to objects
	// that match a query.  See the package comments for details.
	watch.WatcherService
	// PutMutations atomically commits a stream of Mutations when the stream is
	// closed. Mutations are not committed if the request is cancelled before
	// the stream has been closed.
	PutMutations(context _gen_ipc.ServerContext, stream StoreServicePutMutationsStream) (err error)
}

// StorePutMutationsStream is the interface for streaming responses of the method
// PutMutations in the service interface Store.
type StorePutMutationsStream interface {

	// Send places the item onto the output stream, blocking if there is no buffer
	// space available.
	Send(item Mutation) error

	// CloseSend indicates to the server that no more items will be sent; server
	// Recv calls will receive io.EOF after all sent items.  Subsequent calls to
	// Send on the client will fail.  This is an optional call - it's used by
	// streaming clients that need the server to receive the io.EOF terminator.
	CloseSend() error

	// Finish closes the stream and returns the positional return values for
	// call.
	Finish() (err error)

	// Cancel cancels the RPC, notifying the server to stop processing.
	Cancel()
}

// Implementation of the StorePutMutationsStream interface that is not exported.
type implStorePutMutationsStream struct {
	clientCall _gen_ipc.Call
}

func (c *implStorePutMutationsStream) Send(item Mutation) error {
	return c.clientCall.Send(item)
}

func (c *implStorePutMutationsStream) CloseSend() error {
	return c.clientCall.CloseSend()
}

func (c *implStorePutMutationsStream) Finish() (err error) {
	if ierr := c.clientCall.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (c *implStorePutMutationsStream) Cancel() {
	c.clientCall.Cancel()
}

// StoreServicePutMutationsStream is the interface for streaming responses of the method
// PutMutations in the service interface Store.
type StoreServicePutMutationsStream interface {

	// Recv fills itemptr with the next item in the input stream, blocking until
	// an item is available.  Returns io.EOF to indicate graceful end of input.
	Recv() (item Mutation, err error)
}

// Implementation of the StoreServicePutMutationsStream interface that is not exported.
type implStoreServicePutMutationsStream struct {
	serverCall _gen_ipc.ServerCall
}

func (s *implStoreServicePutMutationsStream) Recv() (item Mutation, err error) {
	err = s.serverCall.Recv(&item)
	return
}

// BindStore returns the client stub implementing the Store
// interface.
//
// If no _gen_ipc.Client is specified, the default _gen_ipc.Client in the
// global Runtime is used.
func BindStore(name string, opts ..._gen_ipc.BindOpt) (Store, error) {
	var client _gen_ipc.Client
	switch len(opts) {
	case 0:
		client = _gen_rt.R().Client()
	case 1:
		switch o := opts[0].(type) {
		case _gen_veyron2.Runtime:
			client = o.Client()
		case _gen_ipc.Client:
			client = o
		default:
			return nil, _gen_vdl.ErrUnrecognizedOption
		}
	default:
		return nil, _gen_vdl.ErrTooManyOptionsToBind
	}
	stub := &clientStubStore{client: client, name: name}
	stub.Watcher_ExcludingUniversal, _ = watch.BindWatcher(name, client)

	return stub, nil
}

// NewServerStore creates a new server stub.
//
// It takes a regular server implementing the StoreService
// interface, and returns a new server stub.
func NewServerStore(server StoreService) interface{} {
	return &ServerStubStore{
		ServerStubWatcher: *watch.NewServerWatcher(server).(*watch.ServerStubWatcher),
		service:           server,
	}
}

// clientStubStore implements Store.
type clientStubStore struct {
	watch.Watcher_ExcludingUniversal

	client _gen_ipc.Client
	name   string
}

func (__gen_c *clientStubStore) PutMutations(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply StorePutMutationsStream, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client.StartCall(ctx, __gen_c.name, "PutMutations", nil, opts...); err != nil {
		return
	}
	reply = &implStorePutMutationsStream{clientCall: call}
	return
}

func (__gen_c *clientStubStore) UnresolveStep(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply []string, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client.StartCall(ctx, __gen_c.name, "UnresolveStep", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubStore) Signature(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply _gen_ipc.ServiceSignature, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client.StartCall(ctx, __gen_c.name, "Signature", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubStore) GetMethodTags(ctx _gen_context.T, method string, opts ..._gen_ipc.CallOpt) (reply []interface{}, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client.StartCall(ctx, __gen_c.name, "GetMethodTags", []interface{}{method}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

// ServerStubStore wraps a server that implements
// StoreService and provides an object that satisfies
// the requirements of veyron2/ipc.ReflectInvoker.
type ServerStubStore struct {
	watch.ServerStubWatcher

	service StoreService
}

func (__gen_s *ServerStubStore) GetMethodTags(call _gen_ipc.ServerCall, method string) ([]interface{}, error) {
	// TODO(bprosnitz) GetMethodTags() will be replaces with Signature().
	// Note: This exhibits some weird behavior like returning a nil error if the method isn't found.
	// This will change when it is replaced with Signature().
	if resp, err := __gen_s.ServerStubWatcher.GetMethodTags(call, method); resp != nil || err != nil {
		return resp, err
	}
	switch method {
	case "PutMutations":
		return []interface{}{}, nil
	default:
		return nil, nil
	}
}

func (__gen_s *ServerStubStore) Signature(call _gen_ipc.ServerCall) (_gen_ipc.ServiceSignature, error) {
	result := _gen_ipc.ServiceSignature{Methods: make(map[string]_gen_ipc.MethodSignature)}
	result.Methods["PutMutations"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 65},
		},
		InStream: 75,
	}

	result.TypeDefs = []_gen_vdl.Any{
		_gen_wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}, _gen_wiretype.NamedPrimitiveType{Type: 0x32, Name: "byte", Tags: []string(nil)}, _gen_wiretype.ArrayType{Elem: 0x42, Len: 0x10, Name: "veyron2/storage.ID", Tags: []string(nil)}, _gen_wiretype.NamedPrimitiveType{Type: 0x35, Name: "veyron2/storage.Version", Tags: []string(nil)}, _gen_wiretype.NamedPrimitiveType{Type: 0x1, Name: "anydata", Tags: []string(nil)}, _gen_wiretype.NamedPrimitiveType{Type: 0x32, Name: "veyron2/storage.TagOp", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x46, Name: "Op"},
				_gen_wiretype.FieldType{Type: 0x43, Name: "ACL"},
			},
			"veyron2/storage.Tag", []string(nil)},
		_gen_wiretype.SliceType{Elem: 0x47, Name: "veyron2/storage.TagList", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x3, Name: "Name"},
				_gen_wiretype.FieldType{Type: 0x43, Name: "ID"},
			},
			"veyron2/storage.DEntry", []string(nil)},
		_gen_wiretype.SliceType{Elem: 0x49, Name: "", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x43, Name: "ID"},
				_gen_wiretype.FieldType{Type: 0x44, Name: "PriorVersion"},
				_gen_wiretype.FieldType{Type: 0x44, Name: "Version"},
				_gen_wiretype.FieldType{Type: 0x2, Name: "IsRoot"},
				_gen_wiretype.FieldType{Type: 0x45, Name: "Value"},
				_gen_wiretype.FieldType{Type: 0x48, Name: "Tags"},
				_gen_wiretype.FieldType{Type: 0x4a, Name: "Dir"},
			},
			"veyron/services/store/raw.Mutation", []string(nil)},
	}
	var ss _gen_ipc.ServiceSignature
	var firstAdded int
	ss, _ = __gen_s.ServerStubWatcher.Signature(call)
	firstAdded = len(result.TypeDefs)
	for k, v := range ss.Methods {
		for i, _ := range v.InArgs {
			if v.InArgs[i].Type >= _gen_wiretype.TypeIDFirst {
				v.InArgs[i].Type += _gen_wiretype.TypeID(firstAdded)
			}
		}
		for i, _ := range v.OutArgs {
			if v.OutArgs[i].Type >= _gen_wiretype.TypeIDFirst {
				v.OutArgs[i].Type += _gen_wiretype.TypeID(firstAdded)
			}
		}
		if v.InStream >= _gen_wiretype.TypeIDFirst {
			v.InStream += _gen_wiretype.TypeID(firstAdded)
		}
		if v.OutStream >= _gen_wiretype.TypeIDFirst {
			v.OutStream += _gen_wiretype.TypeID(firstAdded)
		}
		result.Methods[k] = v
	}
	//TODO(bprosnitz) combine type definitions from embeded interfaces in a way that doesn't cause duplication.
	for _, d := range ss.TypeDefs {
		switch wt := d.(type) {
		case _gen_wiretype.SliceType:
			if wt.Elem >= _gen_wiretype.TypeIDFirst {
				wt.Elem += _gen_wiretype.TypeID(firstAdded)
			}
			d = wt
		case _gen_wiretype.ArrayType:
			if wt.Elem >= _gen_wiretype.TypeIDFirst {
				wt.Elem += _gen_wiretype.TypeID(firstAdded)
			}
			d = wt
		case _gen_wiretype.MapType:
			if wt.Key >= _gen_wiretype.TypeIDFirst {
				wt.Key += _gen_wiretype.TypeID(firstAdded)
			}
			if wt.Elem >= _gen_wiretype.TypeIDFirst {
				wt.Elem += _gen_wiretype.TypeID(firstAdded)
			}
			d = wt
		case _gen_wiretype.StructType:
			for i, fld := range wt.Fields {
				if fld.Type >= _gen_wiretype.TypeIDFirst {
					wt.Fields[i].Type += _gen_wiretype.TypeID(firstAdded)
				}
			}
			d = wt
			// NOTE: other types are missing, but we are upgrading anyways.
		}
		result.TypeDefs = append(result.TypeDefs, d)
	}

	return result, nil
}

func (__gen_s *ServerStubStore) UnresolveStep(call _gen_ipc.ServerCall) (reply []string, err error) {
	if unresolver, ok := __gen_s.service.(_gen_ipc.Unresolver); ok {
		return unresolver.UnresolveStep(call)
	}
	if call.Server() == nil {
		return
	}
	var published []string
	if published, err = call.Server().Published(); err != nil || published == nil {
		return
	}
	reply = make([]string, len(published))
	for i, p := range published {
		reply[i] = _gen_naming.Join(p, call.Name())
	}
	return
}

func (__gen_s *ServerStubStore) PutMutations(call _gen_ipc.ServerCall) (err error) {
	stream := &implStoreServicePutMutationsStream{serverCall: call}
	err = __gen_s.service.PutMutations(call, stream)
	return
}

// This file was auto-generated by the veyron vdl tool.
// Source: service.vdl

package raw

import (
	"veyron2/services/watch"

	"veyron2/storage"

	// The non-user imports are prefixed with "_gen_" to prevent collisions.
	_gen_io "io"
	_gen_context "veyron2/context"
	_gen_ipc "veyron2/ipc"
	_gen_naming "veyron2/naming"
	_gen_rt "veyron2/rt"
	_gen_vdlutil "veyron2/vdl/vdlutil"
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
	Value _gen_vdlutil.Any
	// Dir is the implicit directory of this entry, and may contain references
	// to other entries in the store.
	Dir []storage.DEntry
}

// Request specifies how to resume from a previous Watch call.
type Request struct {
	// ResumeMarker specifies how to resume from a previous Watch call.
	// See the ResumeMarker type for detailed comments.
	ResumeMarker watch.ResumeMarker
}

const (
	// The raw Store has Object name "<mount>/.store.raw", where <mount> is the
	// Object name of the mount point.
	RawStoreSuffix = ".store.raw"
)

// TODO(bprosnitz) Remove this line once signatures are updated to use typevals.
// It corrects a bug where _gen_wiretype is unused in VDL pacakges where only bootstrap types are used on interfaces.
const _ = _gen_wiretype.TypeIDInvalid

// Store defines a raw interface for the Veyron store. Mutations can be received
// via the Watcher interface, and committed via PutMutation.
// Store is the interface the client binds and uses.
// Store_ExcludingUniversal is the interface without internal framework-added methods
// to enable embedding without method collisions.  Not to be used directly by clients.
type Store_ExcludingUniversal interface {
	// Watch returns a stream of all changes.
	Watch(ctx _gen_context.T, Req Request, opts ..._gen_ipc.CallOpt) (reply StoreWatchCall, err error)
	// PutMutations atomically commits a stream of Mutations when the stream is
	// closed. Mutations are not committed if the request is cancelled before
	// the stream has been closed.
	PutMutations(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply StorePutMutationsCall, err error)
}
type Store interface {
	_gen_ipc.UniversalServiceMethods
	Store_ExcludingUniversal
}

// StoreService is the interface the server implements.
type StoreService interface {

	// Watch returns a stream of all changes.
	Watch(context _gen_ipc.ServerContext, Req Request, stream StoreServiceWatchStream) (err error)
	// PutMutations atomically commits a stream of Mutations when the stream is
	// closed. Mutations are not committed if the request is cancelled before
	// the stream has been closed.
	PutMutations(context _gen_ipc.ServerContext, stream StoreServicePutMutationsStream) (err error)
}

// StoreWatchCall is the interface for call object of the method
// Watch in the service interface Store.
type StoreWatchCall interface {
	// RecvStream returns the recv portion of the stream
	RecvStream() interface {
		// Advance stages an element so the client can retrieve it
		// with Value.  Advance returns true iff there is an
		// element to retrieve.  The client must call Advance before
		// calling Value. Advance may block if an element is not
		// immediately available.
		Advance() bool

		// Value returns the element that was staged by Advance.
		// Value may panic if Advance returned false or was not
		// called at all.  Value does not block.
		Value() watch.ChangeBatch

		// Err returns a non-nil error iff the stream encountered
		// any errors.  Err does not block.
		Err() error
	}

	// Finish blocks until the server is done and returns the positional
	// return values for call.
	//
	// If Cancel has been called, Finish will return immediately; the output of
	// Finish could either be an error signalling cancelation, or the correct
	// positional return values from the server depending on the timing of the
	// call.
	//
	// Calling Finish is mandatory for releasing stream resources, unless Cancel
	// has been called or any of the other methods return an error.
	// Finish should be called at most once.
	Finish() (err error)

	// Cancel cancels the RPC, notifying the server to stop processing.  It
	// is safe to call Cancel concurrently with any of the other stream methods.
	// Calling Cancel after Finish has returned is a no-op.
	Cancel()
}

type implStoreWatchStreamIterator struct {
	clientCall _gen_ipc.Call
	val        watch.ChangeBatch
	err        error
}

func (c *implStoreWatchStreamIterator) Advance() bool {
	c.val = watch.ChangeBatch{}
	c.err = c.clientCall.Recv(&c.val)
	return c.err == nil
}

func (c *implStoreWatchStreamIterator) Value() watch.ChangeBatch {
	return c.val
}

func (c *implStoreWatchStreamIterator) Err() error {
	if c.err == _gen_io.EOF {
		return nil
	}
	return c.err
}

// Implementation of the StoreWatchCall interface that is not exported.
type implStoreWatchCall struct {
	clientCall _gen_ipc.Call
	readStream implStoreWatchStreamIterator
}

func (c *implStoreWatchCall) RecvStream() interface {
	Advance() bool
	Value() watch.ChangeBatch
	Err() error
} {
	return &c.readStream
}

func (c *implStoreWatchCall) Finish() (err error) {
	if ierr := c.clientCall.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (c *implStoreWatchCall) Cancel() {
	c.clientCall.Cancel()
}

type implStoreServiceWatchStreamSender struct {
	serverCall _gen_ipc.ServerCall
}

func (s *implStoreServiceWatchStreamSender) Send(item watch.ChangeBatch) error {
	return s.serverCall.Send(item)
}

// StoreServiceWatchStream is the interface for streaming responses of the method
// Watch in the service interface Store.
type StoreServiceWatchStream interface {
	// SendStream returns the send portion of the stream.
	SendStream() interface {
		// Send places the item onto the output stream, blocking if there is no buffer
		// space available.  If the client has canceled, an error is returned.
		Send(item watch.ChangeBatch) error
	}
}

// Implementation of the StoreServiceWatchStream interface that is not exported.
type implStoreServiceWatchStream struct {
	writer implStoreServiceWatchStreamSender
}

func (s *implStoreServiceWatchStream) SendStream() interface {
	// Send places the item onto the output stream, blocking if there is no buffer
	// space available.  If the client has canceled, an error is returned.
	Send(item watch.ChangeBatch) error
} {
	return &s.writer
}

// StorePutMutationsCall is the interface for call object of the method
// PutMutations in the service interface Store.
type StorePutMutationsCall interface {

	// SendStream returns the send portion of the stream
	SendStream() interface {
		// Send places the item onto the output stream, blocking if there is no
		// buffer space available.  Calls to Send after having called Close
		// or Cancel will fail.  Any blocked Send calls will be unblocked upon
		// calling Cancel.
		Send(item Mutation) error

		// Close indicates to the server that no more items will be sent;
		// server Recv calls will receive io.EOF after all sent items.  This is
		// an optional call - it's used by streaming clients that need the
		// server to receive the io.EOF terminator before the client calls
		// Finish (for example, if the client needs to continue receiving items
		// from the server after having finished sending).
		// Calls to Close after having called Cancel will fail.
		// Like Send, Close blocks when there's no buffer space available.
		Close() error
	}

	// Finish performs the equivalent of SendStream().Close, then blocks until the server
	// is done, and returns the positional return values for call.
	// If Cancel has been called, Finish will return immediately; the output of
	// Finish could either be an error signalling cancelation, or the correct
	// positional return values from the server depending on the timing of the
	// call.
	//
	// Calling Finish is mandatory for releasing stream resources, unless Cancel
	// has been called or any of the other methods return an error.
	// Finish should be called at most once.
	Finish() (err error)

	// Cancel cancels the RPC, notifying the server to stop processing.  It
	// is safe to call Cancel concurrently with any of the other stream methods.
	// Calling Cancel after Finish has returned is a no-op.
	Cancel()
}

type implStorePutMutationsStreamSender struct {
	clientCall _gen_ipc.Call
}

func (c *implStorePutMutationsStreamSender) Send(item Mutation) error {
	return c.clientCall.Send(item)
}

func (c *implStorePutMutationsStreamSender) Close() error {
	return c.clientCall.CloseSend()
}

// Implementation of the StorePutMutationsCall interface that is not exported.
type implStorePutMutationsCall struct {
	clientCall  _gen_ipc.Call
	writeStream implStorePutMutationsStreamSender
}

func (c *implStorePutMutationsCall) SendStream() interface {
	Send(item Mutation) error
	Close() error
} {
	return &c.writeStream
}

func (c *implStorePutMutationsCall) Finish() (err error) {
	if ierr := c.clientCall.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (c *implStorePutMutationsCall) Cancel() {
	c.clientCall.Cancel()
}

type implStoreServicePutMutationsStreamIterator struct {
	serverCall _gen_ipc.ServerCall
	val        Mutation
	err        error
}

func (s *implStoreServicePutMutationsStreamIterator) Advance() bool {
	s.val = Mutation{}
	s.err = s.serverCall.Recv(&s.val)
	return s.err == nil
}

func (s *implStoreServicePutMutationsStreamIterator) Value() Mutation {
	return s.val
}

func (s *implStoreServicePutMutationsStreamIterator) Err() error {
	if s.err == _gen_io.EOF {
		return nil
	}
	return s.err
}

// StoreServicePutMutationsStream is the interface for streaming responses of the method
// PutMutations in the service interface Store.
type StoreServicePutMutationsStream interface {
	// RecvStream returns the recv portion of the stream
	RecvStream() interface {
		// Advance stages an element so the client can retrieve it
		// with Value.  Advance returns true iff there is an
		// element to retrieve.  The client must call Advance before
		// calling Value.  Advance may block if an element is not
		// immediately available.
		Advance() bool

		// Value returns the element that was staged by Advance.
		// Value may panic if Advance returned false or was not
		// called at all.  Value does not block.
		Value() Mutation

		// Err returns a non-nil error iff the stream encountered
		// any errors.  Err does not block.
		Err() error
	}
}

// Implementation of the StoreServicePutMutationsStream interface that is not exported.
type implStoreServicePutMutationsStream struct {
	reader implStoreServicePutMutationsStreamIterator
}

func (s *implStoreServicePutMutationsStream) RecvStream() interface {
	// Advance stages an element so the client can retrieve it
	// with Value.  Advance returns true iff there is an
	// element to retrieve.  The client must call Advance before
	// calling Value.  The client must call Cancel if it does
	// not iterate through all elements (i.e. until Advance
	// returns false).  Advance may block if an element is not
	// immediately available.
	Advance() bool

	// Value returns the element that was staged by Advance.
	// Value may panic if Advance returned false or was not
	// called at all.  Value does not block.
	Value() Mutation

	// Err returns a non-nil error iff the stream encountered
	// any errors.  Err does not block.
	Err() error
} {
	return &s.reader
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
		case _gen_ipc.Client:
			client = o
		default:
			return nil, _gen_vdlutil.ErrUnrecognizedOption
		}
	default:
		return nil, _gen_vdlutil.ErrTooManyOptionsToBind
	}
	stub := &clientStubStore{client: client, name: name}

	return stub, nil
}

// NewServerStore creates a new server stub.
//
// It takes a regular server implementing the StoreService
// interface, and returns a new server stub.
func NewServerStore(server StoreService) interface{} {
	return &ServerStubStore{
		service: server,
	}
}

// clientStubStore implements Store.
type clientStubStore struct {
	client _gen_ipc.Client
	name   string
}

func (__gen_c *clientStubStore) Watch(ctx _gen_context.T, Req Request, opts ..._gen_ipc.CallOpt) (reply StoreWatchCall, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client.StartCall(ctx, __gen_c.name, "Watch", []interface{}{Req}, opts...); err != nil {
		return
	}
	reply = &implStoreWatchCall{clientCall: call, readStream: implStoreWatchStreamIterator{clientCall: call}}
	return
}

func (__gen_c *clientStubStore) PutMutations(ctx _gen_context.T, opts ..._gen_ipc.CallOpt) (reply StorePutMutationsCall, err error) {
	var call _gen_ipc.Call
	if call, err = __gen_c.client.StartCall(ctx, __gen_c.name, "PutMutations", nil, opts...); err != nil {
		return
	}
	reply = &implStorePutMutationsCall{clientCall: call, writeStream: implStorePutMutationsStreamSender{clientCall: call}}
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
	service StoreService
}

func (__gen_s *ServerStubStore) GetMethodTags(call _gen_ipc.ServerCall, method string) ([]interface{}, error) {
	// TODO(bprosnitz) GetMethodTags() will be replaces with Signature().
	// Note: This exhibits some weird behavior like returning a nil error if the method isn't found.
	// This will change when it is replaced with Signature().
	switch method {
	case "Watch":
		return []interface{}{}, nil
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
			{Name: "", Type: 68},
		},
		InStream: 77,
	}
	result.Methods["Watch"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{
			{Name: "Req", Type: 67},
		},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 68},
		},

		OutStream: 72,
	}

	result.TypeDefs = []_gen_vdlutil.Any{
		_gen_wiretype.NamedPrimitiveType{Type: 0x32, Name: "byte", Tags: []string(nil)}, _gen_wiretype.SliceType{Elem: 0x41, Name: "veyron2/services/watch.ResumeMarker", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x42, Name: "ResumeMarker"},
			},
			"veyron/services/store/raw.Request", []string(nil)},
		_gen_wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}, _gen_wiretype.NamedPrimitiveType{Type: 0x1, Name: "anydata", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x3, Name: "Name"},
				_gen_wiretype.FieldType{Type: 0x24, Name: "State"},
				_gen_wiretype.FieldType{Type: 0x45, Name: "Value"},
				_gen_wiretype.FieldType{Type: 0x42, Name: "ResumeMarker"},
				_gen_wiretype.FieldType{Type: 0x2, Name: "Continued"},
			},
			"veyron2/services/watch.Change", []string(nil)},
		_gen_wiretype.SliceType{Elem: 0x46, Name: "", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x47, Name: "Changes"},
			},
			"veyron2/services/watch.ChangeBatch", []string(nil)},
		_gen_wiretype.ArrayType{Elem: 0x41, Len: 0x10, Name: "veyron2/storage.ID", Tags: []string(nil)}, _gen_wiretype.NamedPrimitiveType{Type: 0x35, Name: "veyron2/storage.Version", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x3, Name: "Name"},
				_gen_wiretype.FieldType{Type: 0x49, Name: "ID"},
			},
			"veyron2/storage.DEntry", []string(nil)},
		_gen_wiretype.SliceType{Elem: 0x4b, Name: "", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x49, Name: "ID"},
				_gen_wiretype.FieldType{Type: 0x4a, Name: "PriorVersion"},
				_gen_wiretype.FieldType{Type: 0x4a, Name: "Version"},
				_gen_wiretype.FieldType{Type: 0x2, Name: "IsRoot"},
				_gen_wiretype.FieldType{Type: 0x45, Name: "Value"},
				_gen_wiretype.FieldType{Type: 0x4c, Name: "Dir"},
			},
			"veyron/services/store/raw.Mutation", []string(nil)},
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

func (__gen_s *ServerStubStore) Watch(call _gen_ipc.ServerCall, Req Request) (err error) {
	stream := &implStoreServiceWatchStream{writer: implStoreServiceWatchStreamSender{serverCall: call}}
	err = __gen_s.service.Watch(call, Req, stream)
	return
}

func (__gen_s *ServerStubStore) PutMutations(call _gen_ipc.ServerCall) (err error) {
	stream := &implStoreServicePutMutationsStream{reader: implStoreServicePutMutationsStreamIterator{serverCall: call}}
	err = __gen_s.service.PutMutations(call, stream)
	return
}

package ipc

import (
	"fmt"
	"path"
	"reflect"

	"veyron.io/proximity/api/services/proximity"

	// Imported VDLs.  Please add a link to all VDLs you care about here,
	// and add all interfaces you care about to the init() function below.
	"veyron/examples/fortune"

	ctx "veyron2/context"
	"veyron2/ipc"
)

func init() {
	registerInterface((*fortune.Fortune)(nil))
	registerInterface((*fortune.FortuneService)(nil))
	registerInterface((*proximity.Proximity)(nil))
	registerInterface((*proximity.ProximityService)(nil))
	registerInterface((*proximity.ProximityScanner)(nil))
	registerInterface((*proximity.ProximityScannerService)(nil))
	registerInterface((*proximity.ProximityAnnouncer)(nil))
	registerInterface((*proximity.ProximityAnnouncerService)(nil))
}

// A list of all registered serviceArgGetter-s.
var register map[string]*serviceArgGetter = make(map[string]*serviceArgGetter)

// registerInterface registers the provided VDL client or server interface
// so that its methods' arguments can be created on-the-fly.
func registerInterface(ifacePtr interface{}) {
	t := reflect.TypeOf(ifacePtr)
	if t.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("expected pointer type for %q, got: %v", ifacePtr, t.Kind()))
	}
	t = t.Elem()
	if t.Kind() != reflect.Interface {
		panic(fmt.Sprintf("expected interface type for %q, got: %v", ifacePtr, t.Kind()))
	}

	contextType := reflect.TypeOf((*ctx.T)(nil)).Elem()
	serverContextType := reflect.TypeOf((*ipc.ServerContext)(nil)).Elem()
	optType := reflect.TypeOf(([]ipc.CallOpt)(nil))
	// Create a new arg getter.
	methods := make(map[string][]*methodArgs)
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		var mArgs methodArgs
		for j := 0; j < m.Type.NumIn(); j++ {
			argType := m.Type.In(j)
			if j == 0 {
				if argType == contextType || argType == serverContextType {
					// context arguments - ignore them.
					continue
				}
			}
			if j == m.Type.NumIn()-1 {
				if argType.Kind() == reflect.Interface { // (service) stream argument.
					if err := fillStreamArgs(argType, &mArgs); err != nil {
						panic(err.Error())
					}
					continue
				}
				if argType == optType { // (client) CallOption argument - ignore it.
					continue
				}
			}
			mArgs.inTypes = append(mArgs.inTypes, argType)
		}
		for j := 0; j < m.Type.NumOut()-1; j++ {
			argType := m.Type.Out(j)
			if j == 0 && argType.Kind() == reflect.Interface { // (client) stream argument
				if err := fillStreamArgs(argType, &mArgs); err != nil {
					panic(err.Error())
				}
				continue
			}
			mArgs.outTypes = append(mArgs.outTypes, argType)
		}
		methods[m.Name] = append(methods[m.Name], &mArgs)
	}
	path := path.Join(t.PkgPath(), t.Name())
	register[path] = &serviceArgGetter{
		methods: methods,
		vdlPath: path,
	}
}

// fillStreamArgs fills in stream argument types for the provided stream.
func fillStreamArgs(stream reflect.Type, mArgs *methodArgs) error {
	// Get the stream send type.
	if mSendStream, ok := stream.MethodByName("SendStream"); ok {
		if mSendStream.Type.NumOut() != 1 {
			return fmt.Errorf("Illegal number of arguments for SendStream method in stream %v", stream)
		}
		mSend, ok := mSendStream.Type.Out(0).MethodByName("Send")
		if !ok {
			return fmt.Errorf("Illegal Send method in SendStream %v", mSendStream)
		}

		if mSend.Type.NumIn() != 1 {
			return fmt.Errorf("Illegal number of arguments for Send method in stream %v", stream)
		}
		mArgs.streamSendType = mSend.Type.In(0)
	}
	// Get the stream recv type.
	if mRecvStream, ok := stream.MethodByName("RecvStream"); ok {
		if mRecvStream.Type.NumOut() != 1 {
			return fmt.Errorf("Illegal number of arguments for RecvStream method in stream %v", stream)
		}
		mRecv, ok := mRecvStream.Type.Out(0).MethodByName("Value")
		if !ok {
			return fmt.Errorf("Illegal Value method in RecvStream %v", mRecvStream)
		}
		if mRecv.Type.NumOut() != 1 {
			return fmt.Errorf("Illegal number of arguments for Value method in stream %v", stream)
		}
		mArgs.streamRecvType = mRecv.Type.Out(0)
	}
	if mArgs.streamSendType == nil && mArgs.streamRecvType == nil {
		return fmt.Errorf("Both stream in and out arguments cannot be nil in stream %v", stream)
	}
	// Get the stream finish types.
	if mFinish, ok := stream.MethodByName("Finish"); ok && mFinish.Type.NumOut() > 1 {
		for i := 0; i < mFinish.Type.NumOut()-1; i++ {
			mArgs.streamFinishTypes = append(mArgs.streamFinishTypes, mFinish.Type.Out(i))
		}
	}
	return nil
}

// serviceArgGetter serves method arguments for a specific service.
type serviceArgGetter struct {
	methods map[string][]*methodArgs
	vdlPath string
}

func (sag *serviceArgGetter) String() (ret string) {
	ret = "VDLPath: " + sag.vdlPath
	for k, v := range sag.methods {
		for _, m := range v {
			ret += "; "
			ret += fmt.Sprintf("Method: %s, Args: %v", k, m)
		}
	}
	return
}

// argGetter serves method arguments for a service object.
// (which may implement multiple services)
type argGetter struct {
	methods map[string][]*methodArgs
}

// newArgGetter returns the argument getter for the provided service object.
func newArgGetter(paths []string) (*argGetter, error) {
	ag := &argGetter{
		methods: make(map[string][]*methodArgs),
	}
	for _, path := range paths {
		sag := register[path]
		if sag == nil {
			return nil, fmt.Errorf("unknown service %s", path)
		}
		for method, args := range sag.methods {
			ag.methods[method] = args
		}
	}
	return ag, nil
}

func (ag *argGetter) String() (ret string) {
	for k, v := range ag.methods {
		for _, m := range v {
			ret += "; "
			ret += fmt.Sprintf("Method: %s, Args: %v", k, m)
		}
	}
	return
}

// FindMethod returns the method type information for the given method, or nil if
// the method doesn't exist.
func (ag *argGetter) FindMethod(method string, numInArgs int) *methodArgs {
	ms, ok := ag.methods[method]
	if !ok {
		return nil
	}
	var m *methodArgs
	for _, mi := range ms {
		if len(mi.inTypes) == numInArgs {
			m = mi
			break
		}
	}
	return m
}

// method contains argument type information for a method belonging to an interface.
type methodArgs struct {
	inTypes           []reflect.Type
	outTypes          []reflect.Type
	streamSendType    reflect.Type
	streamRecvType    reflect.Type
	streamFinishTypes []reflect.Type
}

func (m *methodArgs) String() string {
	in := fmt.Sprintf("[%d]", len(m.inTypes))
	out := fmt.Sprintf("[%d]", len(m.outTypes))
	streamFinish := fmt.Sprintf("[%d]", len(m.streamFinishTypes))
	streamSend := "<nil>"
	streamRecv := "<nil>"
	for idx, t := range m.inTypes {
		if idx > 0 {
			in += ", "
		}
		in += t.Name()
	}
	for idx, t := range m.outTypes {
		if idx > 0 {
			out += ", "
		}
		out += t.Name()
	}
	if m.streamSendType != nil {
		streamSend = m.streamSendType.Name()
	}
	if m.streamRecvType != nil {
		streamRecv = m.streamRecvType.Name()
	}
	for idx, t := range m.streamFinishTypes {
		if idx > 0 {
			streamFinish += ", "
		}
		streamFinish += t.Name()
	}
	return fmt.Sprintf("(In: %s; Out: %s; streamSend: %s; StreamRecv: %s; StreamFinish: %s)", in, out, streamSend, streamRecv, streamFinish)
}

func (m *methodArgs) IsStreaming() bool {
	return m.streamSendType != nil || m.streamRecvType != nil
}

// GenInPtrs returns pointers to instances of all input arguments.
func (m *methodArgs) InPtrs() []interface{} {
	argptrs := make([]interface{}, len(m.inTypes))
	for i, arg := range m.inTypes {
		argptrs[i] = newPtrInstance(arg)
	}
	return argptrs
}

// OutPtrs returns pointers to instances of all output arguments.
func (m *methodArgs) OutPtrs() []interface{} {
	argptrs := make([]interface{}, len(m.outTypes))
	for i, arg := range m.outTypes {
		argptrs[i] = newPtrInstance(arg)
	}
	return argptrs
}

// StreamSendPtr returns a pointer to an instance of a stream send type.
func (m *methodArgs) StreamSendPtr() interface{} {
	return newPtrInstance(m.streamSendType)
}

// StreamRecvPtr returns a pointer to an instance of a stream recv type.
func (m *methodArgs) StreamRecvPtr() interface{} {
	return newPtrInstance(m.streamRecvType)
}

// StreamFinishPtrs returns pointers to instances of stream finish types.
func (m *methodArgs) StreamFinishPtrs() []interface{} {
	argptrs := make([]interface{}, len(m.streamFinishTypes))
	for i, arg := range m.streamFinishTypes {
		argptrs[i] = newPtrInstance(arg)
	}
	return argptrs
}

// newPtrInstance returns the pointer to the new instance of the provided type.
func newPtrInstance(t reflect.Type) interface{} {
	if t == nil {
		return nil
	}
	return reflect.New(t).Interface()
}

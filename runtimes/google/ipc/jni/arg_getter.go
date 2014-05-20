// +build android

package main

import (
	"fmt"
	"path"
	"reflect"
	"strings"

	// Imported IDLs.  Please add a link to all IDLs you care about here,
	// and add all interfaces you care about to the init() function below.
	"veyron/examples/fortune"
	"veyron2/ipc"
)

func init() {
	registerInterface((*fortune.Fortune)(nil))
	registerInterface((*fortune.FortuneService)(nil))
}

// A list of all registered argGetter-s.
var register map[string]*argGetter = make(map[string]*argGetter)

// registerInterface registers the provided IDL client or server interface
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

	// Create a new arg getter.
	methods := make(map[string][]methodInfo)
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		in := make([]reflect.Type, m.Type.NumIn()-1)
		idx := 0
		contextType := reflect.TypeOf((*ipc.Context)(nil)).Elem()
		optType := reflect.TypeOf((*ipc.ClientCallOpt)(nil)).Elem()
		for j := 0; j < m.Type.NumIn(); j++ {
			argType := m.Type.In(j)
			if j == 0 && argType == contextType { // skip the Context argument.
				continue
			}
			if j == m.Type.NumIn()-1 && argType == optType { // skip the CallOption argument.
				continue
			}
			in[idx] = argType
			idx++
		}
		out := make([]reflect.Type, m.Type.NumOut()-1) // skip error argument
		for j := 0; j < m.Type.NumOut()-1; j++ {
			out[j] = m.Type.Out(j)
		}
		mis := methods[m.Name]
		mis = append(mis, methodInfo{
			inTypes:  in,
			outTypes: out,
		})
		methods[m.Name] = mis
	}
	path := path.Join(t.PkgPath(), t.Name())
	register[path] = &argGetter{
		methods: methods,
		idlPath: path,
	}
}

// newPtrInstance returns the pointer to the new instance of the provided type.
func newPtrInstance(t reflect.Type) interface{} {
	return reflect.New(t).Interface()
}

// newArgGetter returns the argument getter for the provided IDL interface.
func newArgGetter(javaIdlIfacePath string) *argGetter {
	return register[strings.Join(strings.Split(javaIdlIfacePath, ".")[1:], "/")]
}

// argGetter serves method arguments for a specific interface.
type argGetter struct {
	methods map[string][]methodInfo
	idlPath string
}

// methodInfo contains argument type information for a method belonging to an interface.
type methodInfo struct {
	inTypes  []reflect.Type
	outTypes []reflect.Type
}

func (m methodInfo) String() string {
	in := fmt.Sprintf("[%d]", len(m.inTypes))
	out := fmt.Sprintf("[%d]", len(m.outTypes))
	for _, t := range m.inTypes {
		in = in + ", " + t.Name()
	}
	for _, t := range m.outTypes {
		out = out + ", " + t.Name()
	}
	return fmt.Sprintf("(%s; %s)", in, out)
}

// findMethod returns the method type information for the given method, or nil if
// the method doesn't exist.
func (ag *argGetter) findMethod(method string, numInArgs int) *methodInfo {
	ms, ok := ag.methods[method]
	if !ok {
		return nil
	}
	var m *methodInfo
	for _, mi := range ms {
		if len(mi.inTypes) == numInArgs {
			m = &mi
			break
		}
	}
	return m
}

// GetInArgTypes returns types of all input arguments for the given method.
func (ag *argGetter) GetInArgTypes(method string, numInArgs int) ([]reflect.Type, error) {
	m := ag.findMethod(method, numInArgs)
	if m == nil {
		return nil, fmt.Errorf("couldn't find method %q with %d args in path %s", method, numInArgs, ag.idlPath)
	}
	return m.inTypes, nil
}

// GenInArgPtrs returns pointers to instances of all input arguments for the given method.
func (ag *argGetter) GetInArgPtrs(method string, numInArgs int) (argptrs []interface{}, err error) {
	m := ag.findMethod(method, numInArgs)
	if m == nil {
		return nil, fmt.Errorf("couldn't find method %q with %d args in path %s", method, numInArgs, ag.idlPath)
	}
	argptrs = make([]interface{}, len(m.inTypes))
	for i, arg := range m.inTypes {
		argptrs[i] = newPtrInstance(arg)
	}
	return
}

// GetOurArgTypes returns types of all output arguments for the given method.
func (ag *argGetter) GetOutArgTypes(method string, numInArgs int) ([]reflect.Type, error) {
	m := ag.findMethod(method, numInArgs)
	if m == nil {
		return nil, fmt.Errorf("couldn't find method %q with %d args in path %s", method, numInArgs, ag.idlPath)
	}
	return m.outTypes, nil
}

// GetOutArgPtrs returns pointers to instances of all output arguments for the given method.
func (ag *argGetter) GetOutArgPtrs(method string, numInArgs int) (argptrs []interface{}, err error) {
	m := ag.findMethod(method, numInArgs)
	if m == nil {
		return nil, fmt.Errorf("couldn't find method %q with %d args in path %s", method, numInArgs, ag.idlPath)
	}
	argptrs = make([]interface{}, len(m.outTypes))
	for i, arg := range m.outTypes {
		argptrs[i] = newPtrInstance(arg)
	}
	return
}

// argGetters is a cache of created argument getters, keyed by IDL interface path.
var argGetters map[string]*argGetter = make(map[string]*argGetter)

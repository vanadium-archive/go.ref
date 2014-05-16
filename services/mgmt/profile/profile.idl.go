// This file was auto-generated by the veyron idl tool.
// Source: profile.idl

// Package profile contains implementation and internal interfaces and
// types used by the implementation of Veyron profiles.
package profile

import (
	"veyron2/services/mgmt/profile"

	// The non-user imports are prefixed with "_gen_" to prevent collisions.
	_gen_veyron2 "veyron2"
	_gen_idl "veyron2/idl"
	_gen_ipc "veyron2/ipc"
	_gen_naming "veyron2/naming"
	_gen_rt "veyron2/rt/r"
	_gen_wiretype "veyron2/wiretype"
)

// Format includes a type (e.g. ELF) and each instance of the format
// has some specific attributes. The key attributes are the target
// operating system (e.g. for ELF this could be one of System V,
// HP-UX, NetBSD, Linux, Solaris, AIX, IRIX, FreeBSD, and OpenBSD) and
// the target instruction set architecture (e.g. for ELF this could be
// one of SPARC, x86, PowerPC, ARM, IA-64, x86-64, and AArch64).
type Format struct {
	Name       string
	Attributes map[string]string
}

// Library describes a shared library that applications may use.
type Library struct {
	// Name is the name of the library.
	Name string
	// MajorVersion is the major version of the library.
	MajorVersion string
	// MinorVersion is the minor version of the library.
	MinorVersion string
}

// Specification is how we represent a profile internally. It should
// provide enough information to allow matching of binaries to nodes.
type Specification struct {
	// Format is the file format of the application binary.
	Format Format
	// Libraries is a set of libraries the application binary depends on.
	Libraries map[Library]struct {
	}
	// A human-friendly concise label for the profile, e.g. "linux-media"
	Label string
	// A human-friendly description of the profile.
	Description string
}

// Profile describes a profile internally. Besides the public Profile
// interface, it allows to access and manage the actual profile
// implementation information.
// Profile is the interface the client binds and uses.
// Profile_InternalNoTagGetter is the interface without the TagGetter
// and UnresolveStep methods (both framework-added, rathern than user-defined),
// to enable embedding without method collisions.  Not to be used directly by
// clients.
type Profile_InternalNoTagGetter interface {
	profile.Profile_InternalNoTagGetter

	// Specification returns the profile specification for the profile
	// identified through the veyron name suffix.
	Specification(opts ..._gen_ipc.ClientCallOpt) (reply Specification, err error)

	// Put sets the profile specification for the profile identified
	// through the veyron name suffix.
	Put(Specification Specification, opts ..._gen_ipc.ClientCallOpt) (err error)

	// Remove removes the profile specification for the profile
	// identified through the veyron name suffix.
	Remove(opts ..._gen_ipc.ClientCallOpt) (err error)
}
type Profile interface {
	_gen_idl.TagGetter
	// UnresolveStep returns the names for the remote service, rooted at the
	// service's immediate namespace ancestor.
	UnresolveStep(opts ..._gen_ipc.ClientCallOpt) ([]string, error)
	Profile_InternalNoTagGetter
}

// ProfileService is the interface the server implements.
type ProfileService interface {
	profile.ProfileService

	// Specification returns the profile specification for the profile
	// identified through the veyron name suffix.
	Specification(context _gen_ipc.Context) (reply Specification, err error)

	// Put sets the profile specification for the profile identified
	// through the veyron name suffix.
	Put(context _gen_ipc.Context, Specification Specification) (err error)

	// Remove removes the profile specification for the profile
	// identified through the veyron name suffix.
	Remove(context _gen_ipc.Context) (err error)
}

// BindProfile returns the client stub implementing the Profile
// interface.
//
// If no _gen_ipc.Client is specified, the default _gen_ipc.Client in the
// global Runtime is used.
func BindProfile(name string, opts ..._gen_ipc.BindOpt) (Profile, error) {
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
			return nil, _gen_idl.ErrUnrecognizedOption
		}
	default:
		return nil, _gen_idl.ErrTooManyOptionsToBind
	}
	stub := &clientStubProfile{client: client, name: name}
	stub.Profile_InternalNoTagGetter, _ = profile.BindProfile(name, client)

	return stub, nil
}

// NewServerProfile creates a new server stub.
//
// It takes a regular server implementing the ProfileService
// interface, and returns a new server stub.
func NewServerProfile(server ProfileService) interface{} {
	return &ServerStubProfile{
		ServerStubProfile: *profile.NewServerProfile(server).(*profile.ServerStubProfile),
		service:           server,
	}
}

// clientStubProfile implements Profile.
type clientStubProfile struct {
	profile.Profile_InternalNoTagGetter

	client _gen_ipc.Client
	name   string
}

func (c *clientStubProfile) GetMethodTags(method string) []interface{} {
	return GetProfileMethodTags(method)
}

func (__gen_c *clientStubProfile) Specification(opts ..._gen_ipc.ClientCallOpt) (reply Specification, err error) {
	var call _gen_ipc.ClientCall
	if call, err = __gen_c.client.StartCall(__gen_c.name, "Specification", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubProfile) Put(Specification Specification, opts ..._gen_ipc.ClientCallOpt) (err error) {
	var call _gen_ipc.ClientCall
	if call, err = __gen_c.client.StartCall(__gen_c.name, "Put", []interface{}{Specification}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (__gen_c *clientStubProfile) Remove(opts ..._gen_ipc.ClientCallOpt) (err error) {
	var call _gen_ipc.ClientCall
	if call, err = __gen_c.client.StartCall(__gen_c.name, "Remove", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (c *clientStubProfile) UnresolveStep(opts ..._gen_ipc.ClientCallOpt) (reply []string, err error) {
	var call _gen_ipc.ClientCall
	if call, err = c.client.StartCall(c.name, "UnresolveStep", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&reply, &err); ierr != nil {
		err = ierr
	}
	return
}

// ServerStubProfile wraps a server that implements
// ProfileService and provides an object that satisfies
// the requirements of veyron2/ipc.ReflectInvoker.
type ServerStubProfile struct {
	profile.ServerStubProfile

	service ProfileService
}

func (s *ServerStubProfile) GetMethodTags(method string) []interface{} {
	return GetProfileMethodTags(method)
}

func (s *ServerStubProfile) Signature(call _gen_ipc.ServerCall) (_gen_ipc.ServiceSignature, error) {
	result := _gen_ipc.ServiceSignature{Methods: make(map[string]_gen_ipc.MethodSignature)}
	result.Methods["Put"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{
			{Name: "Specification", Type: 70},
		},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 71},
		},
	}
	result.Methods["Remove"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 71},
		},
	}
	result.Methods["Specification"] = _gen_ipc.MethodSignature{
		InArgs: []_gen_ipc.MethodArgument{},
		OutArgs: []_gen_ipc.MethodArgument{
			{Name: "", Type: 70},
			{Name: "", Type: 71},
		},
	}

	result.TypeDefs = []_gen_idl.AnyData{
		_gen_wiretype.MapType{Key: 0x3, Elem: 0x3, Name: "", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x3, Name: "Name"},
				_gen_wiretype.FieldType{Type: 0x41, Name: "Attributes"},
			},
			"Format", []string(nil)},
		_gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x3, Name: "Name"},
				_gen_wiretype.FieldType{Type: 0x3, Name: "MajorVersion"},
				_gen_wiretype.FieldType{Type: 0x3, Name: "MinorVersion"},
			},
			"Library", []string(nil)},
		_gen_wiretype.StructType{
			nil,
			"", []string(nil)},
		_gen_wiretype.MapType{Key: 0x43, Elem: 0x44, Name: "", Tags: []string(nil)}, _gen_wiretype.StructType{
			[]_gen_wiretype.FieldType{
				_gen_wiretype.FieldType{Type: 0x42, Name: "Format"},
				_gen_wiretype.FieldType{Type: 0x45, Name: "Libraries"},
				_gen_wiretype.FieldType{Type: 0x3, Name: "Label"},
				_gen_wiretype.FieldType{Type: 0x3, Name: "Description"},
			},
			"Specification", []string(nil)},
		_gen_wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}
	var ss _gen_ipc.ServiceSignature
	var firstAdded int
	ss, _ = s.ServerStubProfile.Signature(call)
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
			for _, fld := range wt.Fields {
				if fld.Type >= _gen_wiretype.TypeIDFirst {
					fld.Type += _gen_wiretype.TypeID(firstAdded)
				}
			}
			d = wt
		}
		result.TypeDefs = append(result.TypeDefs, d)
	}

	return result, nil
}

func (s *ServerStubProfile) UnresolveStep(call _gen_ipc.ServerCall) (reply []string, err error) {
	if unresolver, ok := s.service.(_gen_ipc.Unresolver); ok {
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

func (__gen_s *ServerStubProfile) Specification(call _gen_ipc.ServerCall) (reply Specification, err error) {
	reply, err = __gen_s.service.Specification(call)
	return
}

func (__gen_s *ServerStubProfile) Put(call _gen_ipc.ServerCall, Specification Specification) (err error) {
	err = __gen_s.service.Put(call, Specification)
	return
}

func (__gen_s *ServerStubProfile) Remove(call _gen_ipc.ServerCall) (err error) {
	err = __gen_s.service.Remove(call)
	return
}

func GetProfileMethodTags(method string) []interface{} {
	if resp := profile.GetProfileMethodTags(method); resp != nil {
		return resp
	}
	switch method {
	case "Specification":
		return []interface{}{}
	case "Put":
		return []interface{}{}
	case "Remove":
		return []interface{}{}
	default:
		return nil
	}
}

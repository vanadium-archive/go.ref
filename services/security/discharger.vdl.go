// This file was auto-generated by the veyron vdl tool.
// Source: discharger.vdl

package security

import (
	"v.io/core/veyron2/security"

	// The non-user imports are prefixed with "__" to prevent collisions.
	__veyron2 "v.io/core/veyron2"
	__context "v.io/core/veyron2/context"
	__ipc "v.io/core/veyron2/ipc"
	__vdlutil "v.io/core/veyron2/vdl/vdlutil"
	__wiretype "v.io/core/veyron2/wiretype"
)

// TODO(toddw): Remove this line once the new signature support is done.
// It corrects a bug where __wiretype is unused in VDL pacakges where only
// bootstrap types are used on interfaces.
const _ = __wiretype.TypeIDInvalid

// DischargerClientMethods is the client interface
// containing Discharger methods.
//
// Discharger is the interface for obtaining discharges for ThirdPartyCaveats.
type DischargerClientMethods interface {
	// Discharge is called by a principal that holds a blessing with a third
	// party caveat and seeks to get a discharge that proves the fulfillment of
	// this caveat.
	//
	// Caveat and Discharge are of type ThirdPartyCaveat and Discharge
	// respectively. (not enforced here because vdl does not know these types)
	// TODO(ataly,ashankar): Figure out a VDL representation for ThirdPartyCaveat
	// and Discharge and use those here?
	Discharge(ctx __context.T, Caveat __vdlutil.Any, Impetus security.DischargeImpetus, opts ...__ipc.CallOpt) (Discharge __vdlutil.Any, err error)
}

// DischargerClientStub adds universal methods to DischargerClientMethods.
type DischargerClientStub interface {
	DischargerClientMethods
	__ipc.UniversalServiceMethods
}

// DischargerClient returns a client stub for Discharger.
func DischargerClient(name string, opts ...__ipc.BindOpt) DischargerClientStub {
	var client __ipc.Client
	for _, opt := range opts {
		if clientOpt, ok := opt.(__ipc.Client); ok {
			client = clientOpt
		}
	}
	return implDischargerClientStub{name, client}
}

type implDischargerClientStub struct {
	name   string
	client __ipc.Client
}

func (c implDischargerClientStub) c(ctx __context.T) __ipc.Client {
	if c.client != nil {
		return c.client
	}
	return __veyron2.RuntimeFromContext(ctx).Client()
}

func (c implDischargerClientStub) Discharge(ctx __context.T, i0 __vdlutil.Any, i1 security.DischargeImpetus, opts ...__ipc.CallOpt) (o0 __vdlutil.Any, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Discharge", []interface{}{i0, i1}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

func (c implDischargerClientStub) Signature(ctx __context.T, opts ...__ipc.CallOpt) (o0 __ipc.ServiceSignature, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Signature", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

// DischargerServerMethods is the interface a server writer
// implements for Discharger.
//
// Discharger is the interface for obtaining discharges for ThirdPartyCaveats.
type DischargerServerMethods interface {
	// Discharge is called by a principal that holds a blessing with a third
	// party caveat and seeks to get a discharge that proves the fulfillment of
	// this caveat.
	//
	// Caveat and Discharge are of type ThirdPartyCaveat and Discharge
	// respectively. (not enforced here because vdl does not know these types)
	// TODO(ataly,ashankar): Figure out a VDL representation for ThirdPartyCaveat
	// and Discharge and use those here?
	Discharge(ctx __ipc.ServerContext, Caveat __vdlutil.Any, Impetus security.DischargeImpetus) (Discharge __vdlutil.Any, err error)
}

// DischargerServerStubMethods is the server interface containing
// Discharger methods, as expected by ipc.Server.
// There is no difference between this interface and DischargerServerMethods
// since there are no streaming methods.
type DischargerServerStubMethods DischargerServerMethods

// DischargerServerStub adds universal methods to DischargerServerStubMethods.
type DischargerServerStub interface {
	DischargerServerStubMethods
	// Describe the Discharger interfaces.
	Describe__() []__ipc.InterfaceDesc
	// Signature will be replaced with Describe__.
	Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error)
}

// DischargerServer returns a server stub for Discharger.
// It converts an implementation of DischargerServerMethods into
// an object that may be used by ipc.Server.
func DischargerServer(impl DischargerServerMethods) DischargerServerStub {
	stub := implDischargerServerStub{
		impl: impl,
	}
	// Initialize GlobState; always check the stub itself first, to handle the
	// case where the user has the Glob method defined in their VDL source.
	if gs := __ipc.NewGlobState(stub); gs != nil {
		stub.gs = gs
	} else if gs := __ipc.NewGlobState(impl); gs != nil {
		stub.gs = gs
	}
	return stub
}

type implDischargerServerStub struct {
	impl DischargerServerMethods
	gs   *__ipc.GlobState
}

func (s implDischargerServerStub) Discharge(ctx __ipc.ServerContext, i0 __vdlutil.Any, i1 security.DischargeImpetus) (__vdlutil.Any, error) {
	return s.impl.Discharge(ctx, i0, i1)
}

func (s implDischargerServerStub) Globber() *__ipc.GlobState {
	return s.gs
}

func (s implDischargerServerStub) Describe__() []__ipc.InterfaceDesc {
	return []__ipc.InterfaceDesc{DischargerDesc}
}

// DischargerDesc describes the Discharger interface.
var DischargerDesc __ipc.InterfaceDesc = descDischarger

// descDischarger hides the desc to keep godoc clean.
var descDischarger = __ipc.InterfaceDesc{
	Name:    "Discharger",
	PkgPath: "v.io/core/veyron/services/security",
	Doc:     "// Discharger is the interface for obtaining discharges for ThirdPartyCaveats.",
	Methods: []__ipc.MethodDesc{
		{
			Name: "Discharge",
			Doc:  "// Discharge is called by a principal that holds a blessing with a third\n// party caveat and seeks to get a discharge that proves the fulfillment of\n// this caveat.\n//\n// Caveat and Discharge are of type ThirdPartyCaveat and Discharge\n// respectively. (not enforced here because vdl does not know these types)\n// TODO(ataly,ashankar): Figure out a VDL representation for ThirdPartyCaveat\n// and Discharge and use those here?",
			InArgs: []__ipc.ArgDesc{
				{"Caveat", ``},  // __vdlutil.Any
				{"Impetus", ``}, // security.DischargeImpetus
			},
			OutArgs: []__ipc.ArgDesc{
				{"Discharge", ``}, // __vdlutil.Any
				{"err", ``},       // error
			},
		},
	},
}

func (s implDischargerServerStub) Signature(ctx __ipc.ServerContext) (__ipc.ServiceSignature, error) {
	// TODO(toddw): Replace with new Describe__ implementation.
	result := __ipc.ServiceSignature{Methods: make(map[string]__ipc.MethodSignature)}
	result.Methods["Discharge"] = __ipc.MethodSignature{
		InArgs: []__ipc.MethodArgument{
			{Name: "Caveat", Type: 65},
			{Name: "Impetus", Type: 69},
		},
		OutArgs: []__ipc.MethodArgument{
			{Name: "Discharge", Type: 65},
			{Name: "err", Type: 70},
		},
	}

	result.TypeDefs = []__vdlutil.Any{
		__wiretype.NamedPrimitiveType{Type: 0x1, Name: "anydata", Tags: []string(nil)}, __wiretype.NamedPrimitiveType{Type: 0x3, Name: "v.io/core/veyron2/security.BlessingPattern", Tags: []string(nil)}, __wiretype.SliceType{Elem: 0x42, Name: "", Tags: []string(nil)}, __wiretype.SliceType{Elem: 0x41, Name: "", Tags: []string(nil)}, __wiretype.StructType{
			[]__wiretype.FieldType{
				__wiretype.FieldType{Type: 0x43, Name: "Server"},
				__wiretype.FieldType{Type: 0x3, Name: "Method"},
				__wiretype.FieldType{Type: 0x44, Name: "Arguments"},
			},
			"v.io/core/veyron2/security.DischargeImpetus", []string(nil)},
		__wiretype.NamedPrimitiveType{Type: 0x1, Name: "error", Tags: []string(nil)}}

	return result, nil
}

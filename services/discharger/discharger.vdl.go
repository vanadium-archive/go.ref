// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: discharger

// Package discharger defines an interface for obtaining discharges for
// third-party caveats.
package discharger

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Error definitions
var (

	// Indicates that the Caveat does not require a discharge
	ErrNotAThirdPartyCaveat = verror.Register("v.io/x/ref/services/discharger.NotAThirdPartyCaveat", verror.NoRetry, "{1:}{2:} discharges are not required for non-third-party caveats (id: {c.id})")
)

// NewErrNotAThirdPartyCaveat returns an error with the ErrNotAThirdPartyCaveat ID.
func NewErrNotAThirdPartyCaveat(ctx *context.T, c security.Caveat) error {
	return verror.New(ErrNotAThirdPartyCaveat, ctx, c)
}

//////////////////////////////////////////////////
// Interface definitions

// DischargerClientMethods is the client interface
// containing Discharger methods.
//
// Discharger is the interface for obtaining discharges for ThirdPartyCaveats.
type DischargerClientMethods interface {
	// Discharge is called by a principal that holds a blessing with a third
	// party caveat and seeks to get a discharge that proves the fulfillment of
	// this caveat.
	Discharge(_ *context.T, Caveat security.Caveat, Impetus security.DischargeImpetus, _ ...rpc.CallOpt) (Discharge security.Discharge, _ error)
}

// DischargerClientStub adds universal methods to DischargerClientMethods.
type DischargerClientStub interface {
	DischargerClientMethods
	rpc.UniversalServiceMethods
}

// DischargerClient returns a client stub for Discharger.
func DischargerClient(name string) DischargerClientStub {
	return implDischargerClientStub{name}
}

type implDischargerClientStub struct {
	name string
}

func (c implDischargerClientStub) Discharge(ctx *context.T, i0 security.Caveat, i1 security.DischargeImpetus, opts ...rpc.CallOpt) (o0 security.Discharge, err error) {
	err = v23.GetClient(ctx).Call(ctx, c.name, "Discharge", []interface{}{i0, i1}, []interface{}{&o0}, opts...)
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
	Discharge(_ *context.T, _ rpc.ServerCall, Caveat security.Caveat, Impetus security.DischargeImpetus) (Discharge security.Discharge, _ error)
}

// DischargerServerStubMethods is the server interface containing
// Discharger methods, as expected by rpc.Server.
// There is no difference between this interface and DischargerServerMethods
// since there are no streaming methods.
type DischargerServerStubMethods DischargerServerMethods

// DischargerServerStub adds universal methods to DischargerServerStubMethods.
type DischargerServerStub interface {
	DischargerServerStubMethods
	// Describe the Discharger interfaces.
	Describe__() []rpc.InterfaceDesc
}

// DischargerServer returns a server stub for Discharger.
// It converts an implementation of DischargerServerMethods into
// an object that may be used by rpc.Server.
func DischargerServer(impl DischargerServerMethods) DischargerServerStub {
	stub := implDischargerServerStub{
		impl: impl,
	}
	// Initialize GlobState; always check the stub itself first, to handle the
	// case where the user has the Glob method defined in their VDL source.
	if gs := rpc.NewGlobState(stub); gs != nil {
		stub.gs = gs
	} else if gs := rpc.NewGlobState(impl); gs != nil {
		stub.gs = gs
	}
	return stub
}

type implDischargerServerStub struct {
	impl DischargerServerMethods
	gs   *rpc.GlobState
}

func (s implDischargerServerStub) Discharge(ctx *context.T, call rpc.ServerCall, i0 security.Caveat, i1 security.DischargeImpetus) (security.Discharge, error) {
	return s.impl.Discharge(ctx, call, i0, i1)
}

func (s implDischargerServerStub) Globber() *rpc.GlobState {
	return s.gs
}

func (s implDischargerServerStub) Describe__() []rpc.InterfaceDesc {
	return []rpc.InterfaceDesc{DischargerDesc}
}

// DischargerDesc describes the Discharger interface.
var DischargerDesc rpc.InterfaceDesc = descDischarger

// descDischarger hides the desc to keep godoc clean.
var descDischarger = rpc.InterfaceDesc{
	Name:    "Discharger",
	PkgPath: "v.io/x/ref/services/discharger",
	Doc:     "// Discharger is the interface for obtaining discharges for ThirdPartyCaveats.",
	Methods: []rpc.MethodDesc{
		{
			Name: "Discharge",
			Doc:  "// Discharge is called by a principal that holds a blessing with a third\n// party caveat and seeks to get a discharge that proves the fulfillment of\n// this caveat.",
			InArgs: []rpc.ArgDesc{
				{"Caveat", ``},  // security.Caveat
				{"Impetus", ``}, // security.DischargeImpetus
			},
			OutArgs: []rpc.ArgDesc{
				{"Discharge", ``}, // security.Discharge
			},
		},
	},
}

var __VDLInitCalled bool

// __VDLInit performs vdl initialization.  It is safe to call multiple times.
// If you have an init ordering issue, just insert the following line verbatim
// into your source files in this package, right after the "package foo" clause:
//
//    var _ = __VDLInit()
//
// The purpose of this function is to ensure that vdl initialization occurs in
// the right order, and very early in the init sequence.  In particular, vdl
// registration and package variable initialization needs to occur before
// functions like vdl.TypeOf will work properly.
//
// This function returns a dummy value, so that it can be used to initialize the
// first var in the file, to take advantage of Go's defined init order.
func __VDLInit() struct{} {
	if __VDLInitCalled {
		return struct{}{}
	}

	// Set error format strings.
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(ErrNotAThirdPartyCaveat.ID), "{1:}{2:} discharges are not required for non-third-party caveats (id: {c.id})")

	return struct{}{}
}

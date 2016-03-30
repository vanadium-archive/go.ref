// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: mounttablelib

package mounttablelib

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Interface definitions

// CollectionClientMethods is the client interface
// containing Collection methods.
type CollectionClientMethods interface {
	// Export sets the value for a name.  Overwrite controls the behavior when
	// an entry exists, if Overwrite is true, then the binding is replaced,
	// otherwise the call fails with an error.  The Val must be no larger than
	// MaxSize bytes.
	Export(_ *context.T, Val string, Overwrite bool, _ ...rpc.CallOpt) error
	// Lookup retrieves the value associated with a name.  Returns an error if
	// there is no such binding.
	Lookup(*context.T, ...rpc.CallOpt) ([]byte, error)
}

// CollectionClientStub adds universal methods to CollectionClientMethods.
type CollectionClientStub interface {
	CollectionClientMethods
	rpc.UniversalServiceMethods
}

// CollectionClient returns a client stub for Collection.
func CollectionClient(name string) CollectionClientStub {
	return implCollectionClientStub{name}
}

type implCollectionClientStub struct {
	name string
}

func (c implCollectionClientStub) Export(ctx *context.T, i0 string, i1 bool, opts ...rpc.CallOpt) (err error) {
	err = v23.GetClient(ctx).Call(ctx, c.name, "Export", []interface{}{i0, i1}, nil, opts...)
	return
}

func (c implCollectionClientStub) Lookup(ctx *context.T, opts ...rpc.CallOpt) (o0 []byte, err error) {
	err = v23.GetClient(ctx).Call(ctx, c.name, "Lookup", nil, []interface{}{&o0}, opts...)
	return
}

// CollectionServerMethods is the interface a server writer
// implements for Collection.
type CollectionServerMethods interface {
	// Export sets the value for a name.  Overwrite controls the behavior when
	// an entry exists, if Overwrite is true, then the binding is replaced,
	// otherwise the call fails with an error.  The Val must be no larger than
	// MaxSize bytes.
	Export(_ *context.T, _ rpc.ServerCall, Val string, Overwrite bool) error
	// Lookup retrieves the value associated with a name.  Returns an error if
	// there is no such binding.
	Lookup(*context.T, rpc.ServerCall) ([]byte, error)
}

// CollectionServerStubMethods is the server interface containing
// Collection methods, as expected by rpc.Server.
// There is no difference between this interface and CollectionServerMethods
// since there are no streaming methods.
type CollectionServerStubMethods CollectionServerMethods

// CollectionServerStub adds universal methods to CollectionServerStubMethods.
type CollectionServerStub interface {
	CollectionServerStubMethods
	// Describe the Collection interfaces.
	Describe__() []rpc.InterfaceDesc
}

// CollectionServer returns a server stub for Collection.
// It converts an implementation of CollectionServerMethods into
// an object that may be used by rpc.Server.
func CollectionServer(impl CollectionServerMethods) CollectionServerStub {
	stub := implCollectionServerStub{
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

type implCollectionServerStub struct {
	impl CollectionServerMethods
	gs   *rpc.GlobState
}

func (s implCollectionServerStub) Export(ctx *context.T, call rpc.ServerCall, i0 string, i1 bool) error {
	return s.impl.Export(ctx, call, i0, i1)
}

func (s implCollectionServerStub) Lookup(ctx *context.T, call rpc.ServerCall) ([]byte, error) {
	return s.impl.Lookup(ctx, call)
}

func (s implCollectionServerStub) Globber() *rpc.GlobState {
	return s.gs
}

func (s implCollectionServerStub) Describe__() []rpc.InterfaceDesc {
	return []rpc.InterfaceDesc{CollectionDesc}
}

// CollectionDesc describes the Collection interface.
var CollectionDesc rpc.InterfaceDesc = descCollection

// descCollection hides the desc to keep godoc clean.
var descCollection = rpc.InterfaceDesc{
	Name:    "Collection",
	PkgPath: "v.io/x/ref/services/mounttable/mounttablelib",
	Methods: []rpc.MethodDesc{
		{
			Name: "Export",
			Doc:  "// Export sets the value for a name.  Overwrite controls the behavior when\n// an entry exists, if Overwrite is true, then the binding is replaced,\n// otherwise the call fails with an error.  The Val must be no larger than\n// MaxSize bytes.",
			InArgs: []rpc.ArgDesc{
				{"Val", ``},       // string
				{"Overwrite", ``}, // bool
			},
		},
		{
			Name: "Lookup",
			Doc:  "// Lookup retrieves the value associated with a name.  Returns an error if\n// there is no such binding.",
			OutArgs: []rpc.ArgDesc{
				{"", ``}, // []byte
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
	__VDLInitCalled = true

	return struct{}{}
}

// This file was auto-generated by the veyron vdl tool.
// Source: config.vdl

package device

import (
	// The non-user imports are prefixed with "__" to prevent collisions.
	__veyron2 "v.io/core/veyron2"
	__context "v.io/core/veyron2/context"
	__ipc "v.io/core/veyron2/ipc"
)

// ConfigClientMethods is the client interface
// containing Config methods.
//
// Config is an RPC API to the config service.
type ConfigClientMethods interface {
	// Set sets the value for key.
	Set(ctx *__context.T, key string, value string, opts ...__ipc.CallOpt) error
}

// ConfigClientStub adds universal methods to ConfigClientMethods.
type ConfigClientStub interface {
	ConfigClientMethods
	__ipc.UniversalServiceMethods
}

// ConfigClient returns a client stub for Config.
func ConfigClient(name string, opts ...__ipc.BindOpt) ConfigClientStub {
	var client __ipc.Client
	for _, opt := range opts {
		if clientOpt, ok := opt.(__ipc.Client); ok {
			client = clientOpt
		}
	}
	return implConfigClientStub{name, client}
}

type implConfigClientStub struct {
	name   string
	client __ipc.Client
}

func (c implConfigClientStub) c(ctx *__context.T) __ipc.Client {
	if c.client != nil {
		return c.client
	}
	return __veyron2.GetClient(ctx)
}

func (c implConfigClientStub) Set(ctx *__context.T, i0 string, i1 string, opts ...__ipc.CallOpt) (err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Set", []interface{}{i0, i1}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

// ConfigServerMethods is the interface a server writer
// implements for Config.
//
// Config is an RPC API to the config service.
type ConfigServerMethods interface {
	// Set sets the value for key.
	Set(ctx __ipc.ServerContext, key string, value string) error
}

// ConfigServerStubMethods is the server interface containing
// Config methods, as expected by ipc.Server.
// There is no difference between this interface and ConfigServerMethods
// since there are no streaming methods.
type ConfigServerStubMethods ConfigServerMethods

// ConfigServerStub adds universal methods to ConfigServerStubMethods.
type ConfigServerStub interface {
	ConfigServerStubMethods
	// Describe the Config interfaces.
	Describe__() []__ipc.InterfaceDesc
}

// ConfigServer returns a server stub for Config.
// It converts an implementation of ConfigServerMethods into
// an object that may be used by ipc.Server.
func ConfigServer(impl ConfigServerMethods) ConfigServerStub {
	stub := implConfigServerStub{
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

type implConfigServerStub struct {
	impl ConfigServerMethods
	gs   *__ipc.GlobState
}

func (s implConfigServerStub) Set(ctx __ipc.ServerContext, i0 string, i1 string) error {
	return s.impl.Set(ctx, i0, i1)
}

func (s implConfigServerStub) Globber() *__ipc.GlobState {
	return s.gs
}

func (s implConfigServerStub) Describe__() []__ipc.InterfaceDesc {
	return []__ipc.InterfaceDesc{ConfigDesc}
}

// ConfigDesc describes the Config interface.
var ConfigDesc __ipc.InterfaceDesc = descConfig

// descConfig hides the desc to keep godoc clean.
var descConfig = __ipc.InterfaceDesc{
	Name:    "Config",
	PkgPath: "v.io/core/veyron/services/mgmt/device",
	Doc:     "// Config is an RPC API to the config service.",
	Methods: []__ipc.MethodDesc{
		{
			Name: "Set",
			Doc:  "// Set sets the value for key.",
			InArgs: []__ipc.ArgDesc{
				{"key", ``},   // string
				{"value", ``}, // string
			},
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // error
			},
		},
	},
}

// This file was auto-generated by the veyron vdl tool.
// Source: repository.vdl

// Package repository contains implementation of the interface for
// storing and serving various veyron management objects.
package repository

import (
	"v.io/core/veyron/services/mgmt/profile"

	"v.io/core/veyron2/services/mgmt/application"

	"v.io/core/veyron2/services/mgmt/repository"

	"v.io/core/veyron2/services/security/access"

	// The non-user imports are prefixed with "__" to prevent collisions.
	__veyron2 "v.io/core/veyron2"
	__context "v.io/core/veyron2/context"
	__ipc "v.io/core/veyron2/ipc"
	__vdlutil "v.io/core/veyron2/vdl/vdlutil"
)

// ApplicationClientMethods is the client interface
// containing Application methods.
//
// Application describes an application repository internally. Besides
// the public Application interface, it allows to add and remove
// application envelopes.
type ApplicationClientMethods interface {
	// Application provides access to application envelopes. An
	// application envelope is identified by an application name and an
	// application version, which are specified through the object name,
	// and a profile name, which is specified using a method argument.
	//
	// Example:
	// /apps/search/v1.Match([]string{"base", "media"})
	//   returns an application envelope that can be used for downloading
	//   and executing the "search" application, version "v1", runnable
	//   on either the "base" or "media" profile.
	repository.ApplicationClientMethods
	// Put adds the given tuple of application version (specified
	// through the object name suffix) and application envelope to all
	// of the given application profiles.
	Put(ctx *__context.T, Profiles []string, Envelope application.Envelope, opts ...__ipc.CallOpt) error
	// Remove removes the application envelope for the given profile
	// name and application version (specified through the object name
	// suffix). If no version is specified as part of the suffix, the
	// method removes all versions for the given profile.
	//
	// TODO(jsimsa): Add support for using "*" to specify all profiles
	// when Matt implements Globing (or Ken implements querying).
	Remove(ctx *__context.T, Profile string, opts ...__ipc.CallOpt) error
}

// ApplicationClientStub adds universal methods to ApplicationClientMethods.
type ApplicationClientStub interface {
	ApplicationClientMethods
	__ipc.UniversalServiceMethods
}

// ApplicationClient returns a client stub for Application.
func ApplicationClient(name string, opts ...__ipc.BindOpt) ApplicationClientStub {
	var client __ipc.Client
	for _, opt := range opts {
		if clientOpt, ok := opt.(__ipc.Client); ok {
			client = clientOpt
		}
	}
	return implApplicationClientStub{name, client, repository.ApplicationClient(name, client)}
}

type implApplicationClientStub struct {
	name   string
	client __ipc.Client

	repository.ApplicationClientStub
}

func (c implApplicationClientStub) c(ctx *__context.T) __ipc.Client {
	if c.client != nil {
		return c.client
	}
	return __veyron2.GetClient(ctx)
}

func (c implApplicationClientStub) Put(ctx *__context.T, i0 []string, i1 application.Envelope, opts ...__ipc.CallOpt) (err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Put", []interface{}{i0, i1}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (c implApplicationClientStub) Remove(ctx *__context.T, i0 string, opts ...__ipc.CallOpt) (err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Remove", []interface{}{i0}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

// ApplicationServerMethods is the interface a server writer
// implements for Application.
//
// Application describes an application repository internally. Besides
// the public Application interface, it allows to add and remove
// application envelopes.
type ApplicationServerMethods interface {
	// Application provides access to application envelopes. An
	// application envelope is identified by an application name and an
	// application version, which are specified through the object name,
	// and a profile name, which is specified using a method argument.
	//
	// Example:
	// /apps/search/v1.Match([]string{"base", "media"})
	//   returns an application envelope that can be used for downloading
	//   and executing the "search" application, version "v1", runnable
	//   on either the "base" or "media" profile.
	repository.ApplicationServerMethods
	// Put adds the given tuple of application version (specified
	// through the object name suffix) and application envelope to all
	// of the given application profiles.
	Put(ctx __ipc.ServerContext, Profiles []string, Envelope application.Envelope) error
	// Remove removes the application envelope for the given profile
	// name and application version (specified through the object name
	// suffix). If no version is specified as part of the suffix, the
	// method removes all versions for the given profile.
	//
	// TODO(jsimsa): Add support for using "*" to specify all profiles
	// when Matt implements Globing (or Ken implements querying).
	Remove(ctx __ipc.ServerContext, Profile string) error
}

// ApplicationServerStubMethods is the server interface containing
// Application methods, as expected by ipc.Server.
// There is no difference between this interface and ApplicationServerMethods
// since there are no streaming methods.
type ApplicationServerStubMethods ApplicationServerMethods

// ApplicationServerStub adds universal methods to ApplicationServerStubMethods.
type ApplicationServerStub interface {
	ApplicationServerStubMethods
	// Describe the Application interfaces.
	Describe__() []__ipc.InterfaceDesc
}

// ApplicationServer returns a server stub for Application.
// It converts an implementation of ApplicationServerMethods into
// an object that may be used by ipc.Server.
func ApplicationServer(impl ApplicationServerMethods) ApplicationServerStub {
	stub := implApplicationServerStub{
		impl: impl,
		ApplicationServerStub: repository.ApplicationServer(impl),
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

type implApplicationServerStub struct {
	impl ApplicationServerMethods
	repository.ApplicationServerStub
	gs *__ipc.GlobState
}

func (s implApplicationServerStub) Put(ctx __ipc.ServerContext, i0 []string, i1 application.Envelope) error {
	return s.impl.Put(ctx, i0, i1)
}

func (s implApplicationServerStub) Remove(ctx __ipc.ServerContext, i0 string) error {
	return s.impl.Remove(ctx, i0)
}

func (s implApplicationServerStub) Globber() *__ipc.GlobState {
	return s.gs
}

func (s implApplicationServerStub) Describe__() []__ipc.InterfaceDesc {
	return []__ipc.InterfaceDesc{ApplicationDesc, repository.ApplicationDesc, access.ObjectDesc}
}

// ApplicationDesc describes the Application interface.
var ApplicationDesc __ipc.InterfaceDesc = descApplication

// descApplication hides the desc to keep godoc clean.
var descApplication = __ipc.InterfaceDesc{
	Name:    "Application",
	PkgPath: "v.io/core/veyron/services/mgmt/repository",
	Doc:     "// Application describes an application repository internally. Besides\n// the public Application interface, it allows to add and remove\n// application envelopes.",
	Embeds: []__ipc.EmbedDesc{
		{"Application", "v.io/core/veyron2/services/mgmt/repository", "// Application provides access to application envelopes. An\n// application envelope is identified by an application name and an\n// application version, which are specified through the object name,\n// and a profile name, which is specified using a method argument.\n//\n// Example:\n// /apps/search/v1.Match([]string{\"base\", \"media\"})\n//   returns an application envelope that can be used for downloading\n//   and executing the \"search\" application, version \"v1\", runnable\n//   on either the \"base\" or \"media\" profile."},
	},
	Methods: []__ipc.MethodDesc{
		{
			Name: "Put",
			Doc:  "// Put adds the given tuple of application version (specified\n// through the object name suffix) and application envelope to all\n// of the given application profiles.",
			InArgs: []__ipc.ArgDesc{
				{"Profiles", ``}, // []string
				{"Envelope", ``}, // application.Envelope
			},
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Write")},
		},
		{
			Name: "Remove",
			Doc:  "// Remove removes the application envelope for the given profile\n// name and application version (specified through the object name\n// suffix). If no version is specified as part of the suffix, the\n// method removes all versions for the given profile.\n//\n// TODO(jsimsa): Add support for using \"*\" to specify all profiles\n// when Matt implements Globing (or Ken implements querying).",
			InArgs: []__ipc.ArgDesc{
				{"Profile", ``}, // string
			},
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Write")},
		},
	},
}

// ProfileClientMethods is the client interface
// containing Profile methods.
//
// Profile describes a profile internally. Besides the public Profile
// interface, it allows to add and remove profile specifications.
type ProfileClientMethods interface {
	// Profile abstracts a device's ability to run binaries, and hides
	// specifics such as the operating system, hardware architecture, and
	// the set of installed libraries. Profiles describe binaries and
	// devices, and are used to match them.
	repository.ProfileClientMethods
	// Specification returns the profile specification for the profile
	// identified through the object name suffix.
	Specification(*__context.T, ...__ipc.CallOpt) (profile.Specification, error)
	// Put sets the profile specification for the profile identified
	// through the object name suffix.
	Put(ctx *__context.T, Specification profile.Specification, opts ...__ipc.CallOpt) error
	// Remove removes the profile specification for the profile
	// identified through the object name suffix.
	Remove(*__context.T, ...__ipc.CallOpt) error
}

// ProfileClientStub adds universal methods to ProfileClientMethods.
type ProfileClientStub interface {
	ProfileClientMethods
	__ipc.UniversalServiceMethods
}

// ProfileClient returns a client stub for Profile.
func ProfileClient(name string, opts ...__ipc.BindOpt) ProfileClientStub {
	var client __ipc.Client
	for _, opt := range opts {
		if clientOpt, ok := opt.(__ipc.Client); ok {
			client = clientOpt
		}
	}
	return implProfileClientStub{name, client, repository.ProfileClient(name, client)}
}

type implProfileClientStub struct {
	name   string
	client __ipc.Client

	repository.ProfileClientStub
}

func (c implProfileClientStub) c(ctx *__context.T) __ipc.Client {
	if c.client != nil {
		return c.client
	}
	return __veyron2.GetClient(ctx)
}

func (c implProfileClientStub) Specification(ctx *__context.T, opts ...__ipc.CallOpt) (o0 profile.Specification, err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Specification", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&o0, &err); ierr != nil {
		err = ierr
	}
	return
}

func (c implProfileClientStub) Put(ctx *__context.T, i0 profile.Specification, opts ...__ipc.CallOpt) (err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Put", []interface{}{i0}, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

func (c implProfileClientStub) Remove(ctx *__context.T, opts ...__ipc.CallOpt) (err error) {
	var call __ipc.Call
	if call, err = c.c(ctx).StartCall(ctx, c.name, "Remove", nil, opts...); err != nil {
		return
	}
	if ierr := call.Finish(&err); ierr != nil {
		err = ierr
	}
	return
}

// ProfileServerMethods is the interface a server writer
// implements for Profile.
//
// Profile describes a profile internally. Besides the public Profile
// interface, it allows to add and remove profile specifications.
type ProfileServerMethods interface {
	// Profile abstracts a device's ability to run binaries, and hides
	// specifics such as the operating system, hardware architecture, and
	// the set of installed libraries. Profiles describe binaries and
	// devices, and are used to match them.
	repository.ProfileServerMethods
	// Specification returns the profile specification for the profile
	// identified through the object name suffix.
	Specification(__ipc.ServerContext) (profile.Specification, error)
	// Put sets the profile specification for the profile identified
	// through the object name suffix.
	Put(ctx __ipc.ServerContext, Specification profile.Specification) error
	// Remove removes the profile specification for the profile
	// identified through the object name suffix.
	Remove(__ipc.ServerContext) error
}

// ProfileServerStubMethods is the server interface containing
// Profile methods, as expected by ipc.Server.
// There is no difference between this interface and ProfileServerMethods
// since there are no streaming methods.
type ProfileServerStubMethods ProfileServerMethods

// ProfileServerStub adds universal methods to ProfileServerStubMethods.
type ProfileServerStub interface {
	ProfileServerStubMethods
	// Describe the Profile interfaces.
	Describe__() []__ipc.InterfaceDesc
}

// ProfileServer returns a server stub for Profile.
// It converts an implementation of ProfileServerMethods into
// an object that may be used by ipc.Server.
func ProfileServer(impl ProfileServerMethods) ProfileServerStub {
	stub := implProfileServerStub{
		impl:              impl,
		ProfileServerStub: repository.ProfileServer(impl),
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

type implProfileServerStub struct {
	impl ProfileServerMethods
	repository.ProfileServerStub
	gs *__ipc.GlobState
}

func (s implProfileServerStub) Specification(ctx __ipc.ServerContext) (profile.Specification, error) {
	return s.impl.Specification(ctx)
}

func (s implProfileServerStub) Put(ctx __ipc.ServerContext, i0 profile.Specification) error {
	return s.impl.Put(ctx, i0)
}

func (s implProfileServerStub) Remove(ctx __ipc.ServerContext) error {
	return s.impl.Remove(ctx)
}

func (s implProfileServerStub) Globber() *__ipc.GlobState {
	return s.gs
}

func (s implProfileServerStub) Describe__() []__ipc.InterfaceDesc {
	return []__ipc.InterfaceDesc{ProfileDesc, repository.ProfileDesc}
}

// ProfileDesc describes the Profile interface.
var ProfileDesc __ipc.InterfaceDesc = descProfile

// descProfile hides the desc to keep godoc clean.
var descProfile = __ipc.InterfaceDesc{
	Name:    "Profile",
	PkgPath: "v.io/core/veyron/services/mgmt/repository",
	Doc:     "// Profile describes a profile internally. Besides the public Profile\n// interface, it allows to add and remove profile specifications.",
	Embeds: []__ipc.EmbedDesc{
		{"Profile", "v.io/core/veyron2/services/mgmt/repository", "// Profile abstracts a device's ability to run binaries, and hides\n// specifics such as the operating system, hardware architecture, and\n// the set of installed libraries. Profiles describe binaries and\n// devices, and are used to match them."},
	},
	Methods: []__ipc.MethodDesc{
		{
			Name: "Specification",
			Doc:  "// Specification returns the profile specification for the profile\n// identified through the object name suffix.",
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // profile.Specification
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Read")},
		},
		{
			Name: "Put",
			Doc:  "// Put sets the profile specification for the profile identified\n// through the object name suffix.",
			InArgs: []__ipc.ArgDesc{
				{"Specification", ``}, // profile.Specification
			},
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Write")},
		},
		{
			Name: "Remove",
			Doc:  "// Remove removes the profile specification for the profile\n// identified through the object name suffix.",
			OutArgs: []__ipc.ArgDesc{
				{"", ``}, // error
			},
			Tags: []__vdlutil.Any{access.Tag("Write")},
		},
	},
}

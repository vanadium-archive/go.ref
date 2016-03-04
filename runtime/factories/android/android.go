// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build android

// Package android implements a RuntimeFactory suitable for android.  It is
// based on the roaming package.
//
// The pubsub.Publisher mechanism is used for communicating networking
// settings to the rpc.Server implementation of the runtime and publishes
// the Settings it expects.
package android

import (
	"flag"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/namespace"
	"v.io/v23/rpc"

	"v.io/x/ref/internal/logger"
	dfactory "v.io/x/ref/lib/discovery/factory"
	"v.io/x/ref/lib/flags"
	"v.io/x/ref/lib/pubsub"
	"v.io/x/ref/lib/security/securityflag"
	"v.io/x/ref/runtime/internal"
	"v.io/x/ref/runtime/internal/lib/appcycle"
	"v.io/x/ref/runtime/internal/lib/roaming"
	"v.io/x/ref/runtime/internal/lib/xwebsocket"
	inamespace "v.io/x/ref/runtime/internal/naming/namespace"
	"v.io/x/ref/runtime/internal/rt"
	_ "v.io/x/ref/runtime/protocols/tcp"
	_ "v.io/x/ref/runtime/protocols/ws"
	_ "v.io/x/ref/runtime/protocols/wsh"
	"v.io/x/ref/services/debug/debuglib"
)

var (
	commonFlags      *flags.Flags
	namespaceFactory inamespace.Factory
)

const (
	connIdleExpiry = 15 * time.Second
)

func init() {
	v23.RegisterRuntimeFactory(Init)
	flow.RegisterUnknownProtocol("wsh", xwebsocket.WSH{})
	commonFlags = flags.CreateAndRegister(flag.CommandLine, flags.Runtime, flags.Listen)
}

// Sets the namespace factory to be used for creating all namespaces.
//
// If never invoked, a default namespace factory will be used.  If invoked,
// must be before Init() function below, i.e., before v23.Init().
func SetNamespaceFactory(factory func(*context.T, namespace.T, ...string) (namespace.T, error)) {
	namespaceFactory = inamespace.Factory(factory)
}

func Init(ctx *context.T) (v23.Runtime, *context.T, v23.Shutdown, error) {
	if err := internal.ParseFlagsAndConfigureGlobalLogger(commonFlags); err != nil {
		return nil, nil, nil, err
	}

	ac := appcycle.New()
	discoveryFactory, err := dfactory.New(ctx)
	if err != nil {
		ac.Shutdown()
		return nil, nil, nil, err
	}

	lf := commonFlags.ListenFlags()
	listenSpec := rpc.ListenSpec{
		Addrs:          rpc.ListenAddrs(lf.Addrs),
		Proxy:          lf.Proxy,
		AddressChooser: internal.NewAddressChooser(logger.Global()),
	}
	reservedDispatcher := debuglib.NewDispatcher(securityflag.NewAuthorizerOrDie())

	ishutdown := func() {
		ac.Shutdown()
		discoveryFactory.Shutdown()
	}

	publisher := pubsub.NewPublisher()

	runtime, ctx, shutdown, err := rt.Init(ctx, ac, discoveryFactory, namespaceFactory, nil, &listenSpec, publisher, commonFlags.RuntimeFlags(), reservedDispatcher, connIdleExpiry)
	if err != nil {
		ishutdown()
		return nil, nil, nil, err
	}

	stopRoaming, err := roaming.CreateRoamingStream(ctx, publisher, listenSpec)
	if err != nil {
		return nil, nil, nil, err
	}

	runtimeFactoryShutdown := func() {
		ishutdown()
		shutdown()
		stopRoaming()
	}
	return runtime, ctx, runtimeFactoryShutdown, nil
}

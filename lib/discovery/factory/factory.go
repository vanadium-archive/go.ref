// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"fmt"
	"os"

	"v.io/v23/discovery"

	idiscovery "v.io/x/ref/lib/discovery"
)

// New returns a new Discovery instance with the given protocols.
//
// We instantiate a discovery instance lazily so that we do not turn it on
// until it is actually used.
func New(protocols ...string) (discovery.T, error) {
	if injectedInstance != nil {
		return injectedInstance, nil
	}

	host, _ := os.Hostname()
	if len(host) == 0 {
		// TODO(jhahn): Should we handle error here?
		host = "v23"
	}

	if len(protocols) == 0 {
		// TODO(jhahn): Enable all protocols that are supported by the runtime.
		protocols = []string{"mdns"}
	}

	// Verify protocols.
	for _, p := range protocols {
		if _, exists := pluginFactories[p]; !exists {
			return nil, fmt.Errorf("discovery protocol %q is not supported", p)
		}
	}

	return newLazyFactory(func() (discovery.T, error) { return newInstance(host, protocols) }), nil
}

func newInstance(host string, protocols []string) (discovery.T, error) {
	plugins := make([]idiscovery.Plugin, 0, len(protocols))
	for _, p := range protocols {
		plugin, err := pluginFactories[p](host)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}
	return idiscovery.NewWithPlugins(plugins), nil
}

var injectedInstance discovery.T

// InjectDiscovery allows a runtime to use the given discovery instance. This
// should be called before the runtime is initialized. Mostly used for testing.
func InjectDiscovery(d discovery.T) {
	injectedInstance = d
}

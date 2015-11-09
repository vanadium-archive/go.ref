// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ref defines constants used through the Vanadium reference
// implementation, which is implemented in its subdirectories.
package ref

import (
	"os"
	"strings"
)

const (
	// EnvCredentials is the name of the environment variable pointing to a
	// directory containing all the credentials of a principal (the blessing
	// store, the blessing roots, possibly the private key etc.).
	//
	// Typically only one of EnvCredentials or EnvAgentEndpoint will be set in a
	// process. If both are set, then EnvCredentials takes preference.
	//
	// See v.io/x/ref/lib/security.CreatePersistentPrincipal.
	EnvCredentials = "V23_CREDENTIALS"

	// EnvAgentPath is the name of the environment variable pointing to a socket
	// of the agentd process containing all the credentials for a principal (the
	// blessing store, the blessing roots, possibly the private key etc.).
	//
	// Typically only one of EnvCredentials or EnvAgentPath will be set in a
	// process. If both are set, then EnvCredentials takes preference.
	EnvAgentPath = "V23_AGENT_PATH"

	// EnvAgentEndpoint is the name of the environment variable pointing to an
	// agentd process containing all the credentials a principal (the blessing
	// store, the blessing roots, possibly the private key etc.).
	//
	// EnvAgentEndpoint is deprecated. New agentd processes should use EnvAgentPath.
	// If both are set, EnvAgentPath takes preference.
	EnvAgentEndpoint = "V23_AGENT_ENDPOINT"

	// EnvNamespacePrefix is the prefix of all environment variables that define a
	// namespace root.
	EnvNamespacePrefix = "V23_NAMESPACE"

	// EnvI18nCatalogueFiles is the name of the environment variable pointing to a
	// comma-separated list of i18n catalogue files to be loaded at startup.
	EnvI18nCatalogueFiles = "V23_I18N_CATALOGUE"

	// EnvOAuthIdentityProvider is the name of the environment variable pointing
	// to the url of the OAuth identity provider used by the principal
	// seekblessings command.
	EnvOAuthIdentityProvider = "V23_OAUTH_IDENTITY_PROVIDER"

	// RPCTransitionStateVar is a temporary variable that determines how far along we
	// are in the transition from old to new RPC systems.  It should be removed
	// when the transition is complete.
	RPCTransitionStateVar = "V23_RPC_TRANSITION_STATE"
)

// EnvNamespaceRoots returns the set of namespace roots to be used by the
// process, as specified by environment variables.
//
// It returns both a map of environment variable name to value and the list of
// values.
func EnvNamespaceRoots() (map[string]string, []string) {
	m := make(map[string]string)
	var l []string
	for _, ev := range os.Environ() {
		p := strings.SplitN(ev, "=", 2)
		if len(p) != 2 {
			continue
		}
		k, v := p[0], p[1]
		if strings.HasPrefix(k, EnvNamespacePrefix) && len(v) > 0 {
			l = append(l, v)
			m[k] = v
		}
	}
	return m, l
}

// EnvClearCredentials unsets all environment variables that are used by the
// Runtime to intialize the principal.
func EnvClearCredentials() error {
	for _, v := range []string{
		EnvCredentials,
		EnvAgentEndpoint,
	} {
		if err := os.Unsetenv(v); err != nil {
			return err
		}
	}
	return nil
}

type TransitionState int

const (
	None TransitionState = iota
	XClients
	XServers
)

func RPCTransitionState() TransitionState {
	switch ts := os.Getenv(RPCTransitionStateVar); ts {
	case "xclients", "":
		return XClients
	case "xservers":
		return XServers
	case "none":
		return None
	default:
		panic("Unknown transition state: " + ts)
	}
}

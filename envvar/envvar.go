// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package envvar defines the environment variables used by the reference v23
// implementation.
package envvar

import (
	"os"
	"strings"
)

const (
	// Credentials points to a directory containing all the credentials of
	// a principal (the blessing store, the blessing roots, possibly the
	// private key etc.).
	//
	// Typically only one of Credentials or AgentEndpoint will be set
	// in a process. If both are set, then Credentials takes preference.
	//
	// See v.io/x/ref/lib/security.CreatePersistentPrincipal.
	Credentials = "V23_CREDENTIALS"

	// NamespacePrefix is the prefix of all environment variables that define
	// a namespace root.
	NamespacePrefix = "V23_NAMESPACE"

	// I18nCatalogueFiles points to a comma-separated list of i18n
	// catalogue files to be loaded at startup.
	I18nCatalogueFiles = "V23_I18N_CATALOGUE"
)

// NamespaceRoots returns the set of namespace roots to be used by the process,
// as specified by environment variables.
//
// It returns both a map of environment variable name to value and the list of
// values.
func NamespaceRoots() (map[string]string, []string) {
	m := make(map[string]string)
	var l []string
	for _, ev := range os.Environ() {
		p := strings.SplitN(ev, "=", 2)
		if len(p) != 2 {
			continue
		}
		k, v := p[0], p[1]
		if strings.HasPrefix(k, NamespacePrefix) && len(v) > 0 {
			l = append(l, v)
			m[k] = v
		} else if strings.HasPrefix(k, "NAMESPACE_ROOT") && len(v) > 0 {
			// TODO(ashankar): Remove this once the transition to
			// the new enviornment variables is complete.
			l = append(l, v)
			m[k] = v
		}
	}
	return m, l
}

// ClearCredentials unsets all environment variables that are used by
// the Runtime to intialize the principal.
func ClearCredentials() error {
	for _, v := range []string{
		Credentials,
		// Remove when https://github.com/veyron/release-issues/issues/1597 is closed.
		"VEYRON_AGENT_FD",
	} {
		if err := os.Unsetenv(v); err != nil {
			return err
		}
	}
	return nil
}

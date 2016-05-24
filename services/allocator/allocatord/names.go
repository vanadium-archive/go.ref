// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/conventions"
	"v.io/v23/naming"
	"v.io/v23/security"
)

const (
	// TODO(rthellend): Turn these into flags.
	identityProvider  = "dev.v.io"
	serverMountPrefix = "sb"
)

// newKubeName returns a new kubernetes name.
func newKubeName() (string, error) {
	// Kubernetes names/labels are at most 63 characters long.
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return serverNameFlag + "-" + hex.EncodeToString(b), nil
}

func nameRoot(ctx *context.T) string {
	if serverNameRootFlag != "" {
		return serverNameRootFlag
	}
	if roots := v23.GetNamespace(ctx).Roots(); len(roots) > 0 {
		return roots[0]
	}
	return ""
}

func mountNameFromKubeName(ctx *context.T, kName string) string {
	return naming.Join(nameRoot(ctx), serverMountPrefix, kName)
}

func kubeNameFromMountName(mName string) string {
	if mName == "" {
		return ""
	}
	p := strings.Split(mName, "/")
	return p[len(p)-1]
}

func relativeMountName(mName string) string {
	return naming.Join(serverMountPrefix, kubeNameFromMountName(mName))
}

func emailFromBlessingNames(blessingNames []string) string {
	for _, b := range conventions.ParseBlessingNames(blessingNames...) {
		if b.IdentityProvider != identityProvider {
			continue
		}
		if b.Application != "" {
			continue
		}
		return b.User
	}
	return ""
}

func blessingNamesFromEmail(email string) []string {
	return []string{strings.Join([]string{identityProvider, "u", email}, security.ChainSeparator)}
}

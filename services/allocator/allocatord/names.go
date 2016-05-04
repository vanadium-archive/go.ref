// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/md5"
	"encoding/hex"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/conventions"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/verror"
)

// names returns the mount name and the kubernetes name for the calling user's
// server.
func names(ctx *context.T, call security.Call) (mountName string, kubeName string, err error) {
	home := mountHome(ctx, call)
	if home == "" {
		err = verror.New(verror.ErrBadArg, ctx, "no mount home")
		return
	}

	// Kubernetes names/labels are at most 63 characters long.
	sum := md5.Sum([]byte(home))
	kubeName = serverNameFlag + "-" + hex.EncodeToString(sum[:])

	if roots := v23.GetNamespace(ctx).Roots(); len(roots) > 0 {
		mountName = naming.Join(roots[0], home, serverNameFlag)
	} else {
		mountName = naming.Join(home, serverNameFlag)
	}
	return
}

// mountHome returns the "Home directory" of the calling user.
func mountHome(ctx *context.T, call security.Call) string {
	b, _ := security.RemoteBlessingNames(ctx, call)
	for _, blessing := range conventions.ParseBlessingNames(b...) {
		if home := blessing.Home(); home != "" {
			return home
		}
	}
	return ""
}

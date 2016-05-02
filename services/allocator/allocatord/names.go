// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/md5"
	"encoding/hex"
	"strings"

	"v.io/v23/context"
	"v.io/v23/conventions"
	"v.io/v23/naming"
	"v.io/v23/security"
)

func kubeName(ctx *context.T, call security.Call) string {
	// Kubernetes names/labels are at most 63 characters long.
	sum := md5.Sum([]byte(userId(ctx, call)))
	return serverNameFlag + "-" + hex.EncodeToString(sum[:])
}

func mountName(ctx *context.T, call security.Call) string {
	uid := strings.TrimPrefix(userId(ctx, call), trimMountNamePrefixFlag)
	return naming.Join("users", uid, serverNameFlag)
}

// userId returns the remote user. Uses the same logic as mounttabled to ensure
// that we use the correct mount name.
func userId(ctx *context.T, call security.Call) string {
	ids := conventions.GetClientUserIds(ctx, call)
	return strings.Replace(ids[0], "/", "\\", 0)
}

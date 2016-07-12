// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"strconv"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/common"
	"v.io/x/ref/services/syncbase/store"
)

func FormatVersion(version uint64) string {
	return strconv.FormatUint(version, 10)
}

func CheckVersion(ctx *context.T, presented string, actual uint64) error {
	if presented != "" && presented != FormatVersion(actual) {
		return verror.NewErrBadVersion(ctx)
	}
	return nil
}

// TODO(sadovsky): Perhaps these functions should strip key prefixes such as
// "c:" from the error messages they return.

// GetWithAuth does Get followed by an auth check.
// TODO(ivanpi): Remove when all callers are gone.
func GetWithAuth(ctx *context.T, call rpc.ServerCall, st store.StoreReader, k string, v common.PermserData) error {
	if err := store.Get(ctx, st, k, v); err != nil {
		return err
	}
	auth, _ := access.PermissionsAuthorizer(v.GetPerms(), access.TypicalTagType())
	if err := auth.Authorize(ctx, call.Security()); err != nil {
		return verror.New(verror.ErrNoAccess, ctx, err)
	}
	return nil
}

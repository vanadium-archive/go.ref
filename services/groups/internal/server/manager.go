// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"strings"

	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/groups"
	"v.io/x/ref/services/groups/internal/store"
)

type manager struct {
	st    store.Store
	perms access.Permissions
}

var _ rpc.Dispatcher = (*manager)(nil)

func NewManager(st store.Store, perms access.Permissions) *manager {
	return &manager{st: st, perms: perms}
}

func (m *manager) Lookup(_ *context.T, suffix string) (interface{}, security.Authorizer, error) {
	suffix = strings.TrimPrefix(suffix, "/")
	// TODO(sadovsky): Check that suffix is a valid group name.
	// TODO(sadovsky): Use a real authorizer. Note, this authorizer will be
	// relatively permissive. Stricter access control happens in the individual
	// RPC methods. See syncgroupserver/main.go for example.
	return groups.GroupServer(&group{name: suffix, m: m}), nil, nil
}

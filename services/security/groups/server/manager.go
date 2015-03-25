// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"strings"

	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/security/access"
	"v.io/v23/services/security/groups"
)

type manager struct {
	st  Store
	acl access.Permissions
}

var _ rpc.Dispatcher = (*manager)(nil)

func NewManager(st Store, acl access.Permissions) *manager {
	return &manager{st: st, acl: acl}
}

func (m *manager) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	suffix = strings.TrimPrefix(suffix, "/")
	// TODO(sadovsky): Check that suffix is a valid group name.
	// TODO(sadovsky): Use a real authorizer. Note, this authorizer will be
	// relatively permissive. Stricter access control happens in the individual
	// RPC methods. See syncgroupserver/main.go for example.
	return groups.GroupServer(&group{name: suffix, m: m}), nil, nil
}

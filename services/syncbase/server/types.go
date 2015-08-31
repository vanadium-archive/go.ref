// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/v23/security/access"
)

var (
	_ util.Permser = (*serviceData)(nil)
	_ util.Permser = (*appData)(nil)
)

func (data *serviceData) GetPerms() access.Permissions {
	return data.Perms
}

func (data *appData) GetPerms() access.Permissions {
	return data.Perms
}

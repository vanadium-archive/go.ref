// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	"v.io/v23/security/access"
	"v.io/x/ref/services/syncbase/server/util"
)

var (
	_ util.Permser = (*DatabaseData)(nil)
	_ util.Permser = (*TableData)(nil)
)

func (data *DatabaseData) GetPerms() access.Permissions {
	return data.Perms
}

func (data *TableData) GetPerms() access.Permissions {
	return data.Perms
}

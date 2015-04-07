// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"path/filepath"

	"v.io/v23/security"
	"v.io/v23/security/access"

	"v.io/x/ref/services/internal/acls"
)

// computePath builds the desired path for the debug acls.
func computePath(path string) string {
	return filepath.Join(path, "debugacls")
}

// setACLsForDebugging constructs an ACL file for use by applications that
// permits principals with a Debug right on an application instance to
// access names in the app's __debug space.
func setACLsForDebugging(blessings []string, acl access.Permissions, instancePath string, aclstore *acls.PathStore) error {
	path := computePath(instancePath)
	newACL := make(access.Permissions)

	// Add blessings for the DM so that it can access the app too.

	set := func(bl security.BlessingPattern) {
		for _, tag := range []access.Tag{access.Resolve, access.Debug} {
			newACL.Add(bl, string(tag))
		}
	}

	for _, b := range blessings {
		set(security.BlessingPattern(b))
	}

	// add Resolve for every blessing that has debug
	for _, v := range acl["Debug"].In {
		set(v)
	}
	return aclstore.Set(path, newACL, "")
}

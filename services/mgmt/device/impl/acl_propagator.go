package impl

import (
	"path/filepath"

	"v.io/v23/services/security/access"

	"v.io/x/ref/services/mgmt/lib/acls"
)

// computePath builds the desired path for the debug acls.
func computePath(path string) string {
	return filepath.Join(path, "debugacls")
}

// setACLsForDebugging constructs an ACL file for use by applications that
// permits principals with a Debug right on an application instance to
// access names in the app's __debug space.
func setACLsForDebugging(acl access.Permissions, instancePath string, aclstore *acls.PathStore) error {
	path := computePath(instancePath)
	newACL := make(access.Permissions)
	// add Resolve for every blessing that has debug
	for _, v := range acl["Debug"].In {
		newACL.Add(v, "Resolve")
		newACL.Add(v, "Debug")
	}
	return aclstore.Set(path, newACL, "")
}

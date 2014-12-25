package impl

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"v.io/veyron/veyron2/vlog"
)

const defaultRootPrefix = "veyron_binary_repository"

// SetupRootDir sets up the root directory if it doesn't already exist. If an
// empty string is used as root, create a new temporary directory.
func SetupRootDir(root string) (string, error) {
	if root == "" {
		var err error
		if root, err = ioutil.TempDir("", defaultRootPrefix); err != nil {
			vlog.Errorf("TempDir() failed: %v\n", err)
			return "", err
		}
		path, perm := filepath.Join(root, VersionFile), os.FileMode(0600)
		if err := ioutil.WriteFile(path, []byte(Version), perm); err != nil {
			vlog.Errorf("WriteFile(%v, %v, %v) failed: %v", path, Version, perm, err)
			return "", err
		}
		return root, nil
	}

	_, err := os.Stat(root)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		perm := os.FileMode(0700)
		if err := os.MkdirAll(root, perm); err != nil {
			vlog.Errorf("MkdirAll(%v, %v) failed: %v", root, perm, err)
			return "", err
		}
		path, perm := filepath.Join(root, VersionFile), os.FileMode(0600)
		if err := ioutil.WriteFile(path, []byte(Version), perm); err != nil {
			vlog.Errorf("WriteFile(%v, %v, %v) failed: %v", path, Version, perm, err)
			return "", err
		}
	default:
		vlog.Errorf("Stat(%v) failed: %v", root, err)
		return "", err
	}
	return root, nil
}

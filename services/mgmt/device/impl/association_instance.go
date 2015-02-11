package impl

// Code to manage the persistence of which systemName is associated with
// a given application instance.

import (
	"io/ioutil"
	"path/filepath"

	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/vlog"
)

func saveSystemNameForInstance(dir, systemName string) error {
	snp := filepath.Join(dir, "systemname")
	if err := ioutil.WriteFile(snp, []byte(systemName), 0600); err != nil {
		vlog.Errorf("WriteFile(%v, %v) failed: %v", snp, systemName, err)
		return verror.New(ErrOperationFailed, nil)
	}
	return nil
}

func readSystemNameForInstance(dir string) (string, error) {
	snp := filepath.Join(dir, "systemname")
	name, err := ioutil.ReadFile(snp)
	if err != nil {
		vlog.Errorf("ReadFile(%v) failed: %v", snp, err)
		return "", verror.New(ErrOperationFailed, nil)
	}
	return string(name), nil
}

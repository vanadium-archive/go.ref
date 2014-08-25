// Package impl implements the LogFile interface from
// veyron2/services/mgmt/logreader, which can be used to allow remote access to
// log files, and the Globbable interface from veyron2/services/mounttable to
// find the files in a logs directory.
package impl

import (
	"path/filepath"
	"strings"

	"veyron2/services/mgmt/logreader/types"
	"veyron2/verror"
)

var (
	errCanceled        = verror.Abortedf("operation canceled")
	errNotFound        = verror.NotFoundf("log file not found")
	errEOF             = verror.Make(types.EOF, "EOF")
	errOperationFailed = verror.Internalf("operation failed")
)

// translateNameToFilename returns the file name that corresponds to the object
// name.
func translateNameToFilename(root, name string) (string, error) {
	name = filepath.Join(strings.Split(name, "/")...)
	p := filepath.Join(root, name)
	// Make sure we're not asked to read a file outside of the root
	// directory. This could happen if suffix contains "../", which get
	// collapsed by filepath.Join().
	if !strings.HasPrefix(p, root) {
		return "", errOperationFailed
	}
	return p, nil
}

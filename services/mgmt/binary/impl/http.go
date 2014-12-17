package impl

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	verror "veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/services/mgmt/binary/impl/multipart"
)

// NewHTTPRoot returns an implementation of http.FileSystem that can be used
// to serve the content in the binary service.
func NewHTTPRoot(state *state) http.FileSystem {
	return &httpRoot{state}
}

type httpRoot struct {
	state *state
}

// TODO(caprita): Tie this in with DownloadURL, to control which binaries
// are downloadable via url.

// Open implements http.FileSystem.  It uses the multipart file implementation
// to wrap the content parts into one logical file.
func (r httpRoot) Open(name string) (http.File, error) {
	name = strings.TrimPrefix(name, "/")
	vlog.Infof("HTTP handler opening %s", name)
	parts, err := getParts(r.state.dir(name))
	if err != nil {
		return nil, err
	}
	partFiles := make([]*os.File, len(parts))
	for i, part := range parts {
		if err := checksumExists(part); err != nil {
			return nil, err
		}
		dataPath := filepath.Join(part, data)
		var err error
		if partFiles[i], err = os.Open(dataPath); err != nil {
			vlog.Errorf("Open(%v) failed: %v", dataPath, err)
			return nil, verror.Make(errOperationFailed, nil, dataPath)
		}
	}
	return multipart.NewFile(name, partFiles)
}

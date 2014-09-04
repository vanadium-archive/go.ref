package impl

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"veyron/services/mgmt/lib/binary"

	"veyron2/context"
	"veyron2/services/mgmt/application"
	"veyron2/services/mgmt/repository"
	"veyron2/vlog"
)

func downloadBinary(workspace, fileName, name string) error {
	data, err := binary.Download(name)
	if err != nil {
		vlog.Errorf("Download(%v) failed: %v", name, err)
		return errOperationFailed
	}
	path, perm := filepath.Join(workspace, fileName), os.FileMode(755)
	if err := ioutil.WriteFile(path, data, perm); err != nil {
		vlog.Errorf("WriteFile(%v, %v) failed: %v", path, perm, err)
		return errOperationFailed
	}
	return nil
}

func fetchEnvelope(ctx context.T, origin string) (*application.Envelope, error) {
	stub, err := repository.BindApplication(origin)
	if err != nil {
		vlog.Errorf("BindRepository(%v) failed: %v", origin, err)
		return nil, errOperationFailed
	}
	// TODO(jsimsa): Include logic that computes the set of supported
	// profiles.
	profiles := []string{"test"}
	envelope, err := stub.Match(ctx, profiles)
	if err != nil {
		vlog.Errorf("Match(%v) failed: %v", profiles, err)
		return nil, errOperationFailed
	}
	return &envelope, nil
}

func generateBinary(workspace, fileName string, envelope *application.Envelope, newBinary bool) error {
	if newBinary {
		// Download the new binary.
		return downloadBinary(workspace, fileName, envelope.Binary)
	}
	// Link the current binary.
	path := filepath.Join(workspace, fileName)
	if err := os.Link(os.Args[0], path); err != nil {
		vlog.Errorf("Link(%v, %v) failed: %v", os.Args[0], path, err)
		return errOperationFailed
	}
	return nil
}

func generateVersionDirName() string {
	return time.Now().Format(time.RFC3339Nano)
}

func updateLink(target, link string) error {
	newLink := link + ".new"
	fi, err := os.Lstat(newLink)
	if err == nil {
		if err := os.Remove(fi.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", fi.Name(), err)
			return errOperationFailed
		}
	}
	if err := os.Symlink(target, newLink); err != nil {
		vlog.Errorf("Symlink(%v, %v) failed: %v", target, newLink, err)
		return errOperationFailed
	}
	if err := os.Rename(newLink, link); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", newLink, link, err)
		return errOperationFailed
	}
	return nil
}

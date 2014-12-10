package impl

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"veyron.io/veyron/veyron/services/mgmt/lib/binary"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/services/mgmt/application"
	"veyron.io/veyron/veyron2/services/mgmt/repository"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vlog"
)

// TODO(caprita): Set these timeout in a more principled manner.
const (
	childReadyTimeout = 20 * time.Second
	childWaitTimeout  = 20 * time.Second
	ipcContextTimeout = time.Minute
)

func downloadBinary(ctx context.T, workspace, fileName, name string) error {
	data, _, err := binary.Download(ctx, name)
	if err != nil {
		vlog.Errorf("Download(%v) failed: %v", name, err)
		return verror2.Make(ErrOperationFailed, nil)
	}
	path, perm := filepath.Join(workspace, fileName), os.FileMode(755)
	if err := ioutil.WriteFile(path, data, perm); err != nil {
		vlog.Errorf("WriteFile(%v, %v) failed: %v", path, perm, err)
		return verror2.Make(ErrOperationFailed, nil)
	}
	return nil
}

func fetchEnvelope(ctx context.T, origin string) (*application.Envelope, error) {
	stub := repository.ApplicationClient(origin)
	// TODO(jsimsa): Include logic that computes the set of supported
	// profiles.
	profiles := []string{"test"}
	envelope, err := stub.Match(ctx, profiles)
	if err != nil {
		vlog.Errorf("Match(%v) failed: %v", profiles, err)
		return nil, verror2.Make(ErrOperationFailed, ctx)
	}
	return &envelope, nil
}

// linkSelf creates a link to the current binary.
func linkSelf(workspace, fileName string) error {
	path := filepath.Join(workspace, fileName)
	self := os.Args[0]
	if err := os.Link(self, path); err != nil {
		vlog.Errorf("Link(%v, %v) failed: %v", self, path, err)
		return verror2.Make(ErrOperationFailed, nil)
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
			return verror2.Make(ErrOperationFailed, nil)
		}
	}
	if err := os.Symlink(target, newLink); err != nil {
		vlog.Errorf("Symlink(%v, %v) failed: %v", target, newLink, err)
		return verror2.Make(ErrOperationFailed, nil)
	}
	if err := os.Rename(newLink, link); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", newLink, link, err)
		return verror2.Make(ErrOperationFailed, nil)
	}
	return nil
}

func baseCleanupDir(path, helper string) {
	if helper != "" {
		out, err := exec.Command(helper, "--rm", path).CombinedOutput()
		if err != nil {
			vlog.Errorf("exec.Command(%s %s %s).CombinedOutput() failed: %v", helper, "--rm", path, err)
			return
		}
		if len(out) != 0 {
			vlog.Errorf("exec.Command(%s %s %s).CombinedOutput() generated output: %v", helper, "--rm", path, string(out))
		}
	} else {
		if err := os.RemoveAll(path); err != nil {
			vlog.Errorf("RemoveAll(%v) failed: %v", path, err)
		}
	}
}

// cleanupDir is defined like this so we can override its implementation for
// tests. cleanupDir will use the helper to delete application state possibly
// owned by different accounts if helper is provided.
var cleanupDir = baseCleanupDir

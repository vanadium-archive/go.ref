package impl

import (
	"crypto/sha256"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"v.io/core/veyron/services/mgmt/lib/binary"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/repository"
	"v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"
)

// TODO(caprita): Set these timeout in a more principled manner.
const (
	childReadyTimeout     = 40 * time.Second
	childWaitTimeout      = 40 * time.Second
	ipcContextTimeout     = time.Minute
	ipcContextLongTimeout = 5 * time.Minute
)

func downloadBinary(ctx *context.T, env *application.Envelope, workspace, fileName string) error {
	data, _, err := binary.Download(ctx, env.Binary)
	if err != nil {
		vlog.Errorf("Download(%v) failed: %v", env.Binary, err)
		return verror2.Make(ErrOperationFailed, nil)
	}
	publisher, err := security.NewBlessings(env.Publisher)
	if err != nil {
		vlog.Errorf("Failed to parse publisher blessings:%v", err)
		return verror2.Make(ErrOperationFailed, nil)
	}
	if publisher != nil {
		h := sha256.Sum256(data)
		if !env.Signature.Verify(publisher.PublicKey(), h[:]) {
			vlog.Errorf("Publisher binary(%v) signature mismatch", env.Binary)
			return verror2.Make(ErrOperationFailed, nil)
		}
	}
	path, perm := filepath.Join(workspace, fileName), os.FileMode(755)
	if err := ioutil.WriteFile(path, data, perm); err != nil {
		vlog.Errorf("WriteFile(%v, %v) failed: %v", path, perm, err)
		return verror2.Make(ErrOperationFailed, nil)
	}
	return nil
}

func fetchEnvelope(ctx *context.T, origin string) (*application.Envelope, error) {
	stub := repository.ApplicationClient(origin)
	profilesSet, err := Describe()
	if err != nil {
		vlog.Errorf("Failed to obtain profile labels: %v", err)
		return nil, verror2.Make(ErrOperationFailed, ctx)
	}
	var profiles []string
	for label := range profilesSet.Profiles {
		profiles = append(profiles, label)
	}
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

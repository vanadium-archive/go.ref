package impl

import (
	"crypto/sha256"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"v.io/core/veyron/services/mgmt/device/config"
	"v.io/core/veyron/services/mgmt/lib/binary"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/repository"
	"v.io/core/veyron2/verror"
	"v.io/core/veyron2/vlog"
)

// TODO(caprita): Set these timeout in a more principled manner.
const (
	childReadyTimeout     = 40 * time.Second
	childWaitTimeout      = 40 * time.Second
	ipcContextTimeout     = time.Minute
	ipcContextLongTimeout = 5 * time.Minute
)

func verifySignature(data []byte, publisher security.Blessings, sig security.Signature) error {
	if publisher != nil {
		h := sha256.Sum256(data)
		if !sig.Verify(publisher.PublicKey(), h[:]) {
			return verror.New(ErrOperationFailed, nil)
		}
	}
	return nil
}

func downloadBinary(ctx *context.T, publisher security.Blessings, bin *application.SignedFile, workspace, fileName string) error {
	// TODO(gauthamt): Reduce the number of passes we make over the binary/package
	// data to verify its checksum and signature.
	data, _, err := binary.Download(ctx, bin.File)
	if err != nil {
		vlog.Errorf("Download(%v) failed: %v", bin.File, err)
		return verror.New(ErrOperationFailed, nil)
	}
	if err := verifySignature(data, publisher, bin.Signature); err != nil {
		vlog.Errorf("Publisher binary(%v) signature verification failed", bin.File)
		return err
	}
	path, perm := filepath.Join(workspace, fileName), os.FileMode(0755)
	if err := ioutil.WriteFile(path, data, perm); err != nil {
		vlog.Errorf("WriteFile(%v, %v) failed: %v", path, perm, err)
		return verror.New(ErrOperationFailed, nil)
	}
	return nil
}

// TODO(caprita): share code between downloadBinary and downloadPackages.
func downloadPackages(ctx *context.T, publisher security.Blessings, packages application.Packages, pkgDir string) error {
	for localPkg, pkgName := range packages {
		if localPkg == "" || localPkg[0] == '.' || strings.Contains(localPkg, string(filepath.Separator)) {
			vlog.Infof("invalid local package name: %q", localPkg)
			return verror.New(ErrOperationFailed, nil)
		}
		path := filepath.Join(pkgDir, localPkg)
		if err := binary.DownloadToFile(ctx, pkgName.File, path); err != nil {
			vlog.Infof("DownloadToFile(%q, %q) failed: %v", pkgName, path, err)
			return verror.New(ErrOperationFailed, nil)
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			vlog.Errorf("ReadPackage(%v) failed: %v", path, err)
			return err
		}
		if err := verifySignature(data, publisher, pkgName.Signature); err != nil {
			vlog.Errorf("Publisher package(%v:%v) signature verification failed", localPkg, pkgName)
			return err
		}
	}
	return nil
}

func fetchEnvelope(ctx *context.T, origin string) (*application.Envelope, error) {
	stub := repository.ApplicationClient(origin)
	profilesSet, err := Describe()
	if err != nil {
		vlog.Errorf("Failed to obtain profile labels: %v", err)
		return nil, verror.New(ErrOperationFailed, ctx)
	}
	var profiles []string
	for label := range profilesSet.Profiles {
		profiles = append(profiles, label)
	}
	envelope, err := stub.Match(ctx, profiles)
	if err != nil {
		vlog.Errorf("Match(%v) failed: %v", profiles, err)
		return nil, verror.New(ErrOperationFailed, ctx)
	}
	return &envelope, nil
}

// linkSelf creates a link to the current binary.
func linkSelf(workspace, fileName string) error {
	path := filepath.Join(workspace, fileName)
	self := os.Args[0]
	if err := os.Link(self, path); err != nil {
		vlog.Errorf("Link(%v, %v) failed: %v", self, path, err)
		return verror.New(ErrOperationFailed, nil)
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
			return verror.New(ErrOperationFailed, nil)
		}
	}
	if err := os.Symlink(target, newLink); err != nil {
		vlog.Errorf("Symlink(%v, %v) failed: %v", target, newLink, err)
		return verror.New(ErrOperationFailed, nil)
	}
	if err := os.Rename(newLink, link); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", newLink, link, err)
		return verror.New(ErrOperationFailed, nil)
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

func aclDir(c *config.State) string {
	return filepath.Join(c.Root, "device-manager", "device-data", "acls")
}

// cleanupDir is defined like this so we can override its implementation for
// tests. cleanupDir will use the helper to delete application state possibly
// owned by different accounts if helper is provided.
var cleanupDir = baseCleanupDir

// Package binary provides a client-side library for the binary
// repository.
//
// TODO(jsimsa): Implement parallel download and upload.
package binary

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"v.io/core/veyron2/context"
	"v.io/core/veyron2/services/mgmt/binary"
	"v.io/core/veyron2/services/mgmt/repository"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"

	"v.io/core/veyron/services/mgmt/lib/packages"
)

const pkgPath = "v.io/core/veyron/services/mgmt/lib/binary"

var (
	errOperationFailed = verror.Register(pkgPath+".errOperationFailed", verror.NoRetry, "{1:}{2:} operation failed{:_}")
)

const (
	nAttempts   = 2
	partSize    = 1 << 22
	subpartSize = 1 << 12
)

func Delete(ctx *context.T, name string) error {
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	if err := repository.BinaryClient(name).Delete(ctx); err != nil {
		vlog.Errorf("Delete() failed: %v", err)
		return err
	}
	return nil
}

type indexedPart struct {
	part   binary.PartInfo
	index  int
	offset int64
}

func downloadPartAttempt(ctx *context.T, w io.WriteSeeker, client repository.BinaryClientStub, ip *indexedPart) bool {
	ctx, cancel := ctx.WithCancel()
	defer cancel()

	if _, err := w.Seek(ip.offset, 0); err != nil {
		vlog.Errorf("Seek(%v, 0) failed: %v", ip.offset, err)
		return false
	}
	stream, err := client.Download(ctx, int32(ip.index))
	if err != nil {
		vlog.Errorf("Download(%v) failed: %v", ip.index, err)
		return false
	}
	h, nreceived := md5.New(), 0
	rStream := stream.RecvStream()
	for rStream.Advance() {
		bytes := rStream.Value()
		if _, err := w.Write(bytes); err != nil {
			vlog.Errorf("Write() failed: %v", err)
			return false
		}
		h.Write(bytes)
		nreceived += len(bytes)
	}

	if err := rStream.Err(); err != nil {
		vlog.Errorf("Advance() failed: %v", err)
		return false
	}
	if err := stream.Finish(); err != nil {
		vlog.Errorf("Finish() failed: %v", err)
		return false
	}
	if expected, got := ip.part.Checksum, hex.EncodeToString(h.Sum(nil)); expected != got {
		vlog.Errorf("Unexpected checksum: expected %v, got %v", expected, got)
		return false
	}
	if expected, got := ip.part.Size, int64(nreceived); expected != got {
		vlog.Errorf("Unexpected size: expected %v, got %v", expected, got)
		return false
	}
	return true
}

func downloadPart(ctx *context.T, w io.WriteSeeker, client repository.BinaryClientStub, ip *indexedPart) bool {
	for i := 0; i < nAttempts; i++ {
		if downloadPartAttempt(ctx, w, client, ip) {
			return true
		}
	}
	return false
}

func download(ctx *context.T, w io.WriteSeeker, von string) (repository.MediaInfo, error) {
	client := repository.BinaryClient(von)
	parts, mediaInfo, err := client.Stat(ctx)
	if err != nil {
		vlog.Errorf("Stat() failed: %v", err)
		return repository.MediaInfo{}, err
	}
	for _, part := range parts {
		if part.Checksum == binary.MissingChecksum {
			return repository.MediaInfo{}, verror.Make(verror.NoExist, ctx)
		}
	}
	offset := int64(0)
	for i, part := range parts {
		ip := &indexedPart{part, i, offset}
		if !downloadPart(ctx, w, client, ip) {
			return repository.MediaInfo{}, verror.Make(errOperationFailed, ctx)
		}
		offset += part.Size
	}
	return mediaInfo, nil
}

func Download(ctx *context.T, von string) ([]byte, repository.MediaInfo, error) {
	dir, prefix := "", ""
	file, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		vlog.Errorf("TempFile(%v, %v) failed: %v", dir, prefix, err)
		return nil, repository.MediaInfo{}, verror.Make(errOperationFailed, ctx)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	mediaInfo, err := download(ctx, file, von)
	if err != nil {
		return nil, repository.MediaInfo{}, verror.Make(errOperationFailed, ctx)
	}
	bytes, err := ioutil.ReadFile(file.Name())
	if err != nil {
		vlog.Errorf("ReadFile(%v) failed: %v", file.Name(), err)
		return nil, repository.MediaInfo{}, verror.Make(errOperationFailed, ctx)
	}
	return bytes, mediaInfo, nil
}

func DownloadToFile(ctx *context.T, von, path string) error {
	dir, prefix := "", ""
	file, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		vlog.Errorf("TempFile(%v, %v) failed: %v", dir, prefix, err)
		return verror.Make(errOperationFailed, ctx)
	}
	defer file.Close()
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	mediaInfo, err := download(ctx, file, von)
	if err != nil {
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return verror.Make(errOperationFailed, ctx)
	}
	perm := os.FileMode(0600)
	if err := file.Chmod(perm); err != nil {
		vlog.Errorf("Chmod(%v) failed: %v", perm, err)
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return verror.Make(errOperationFailed, ctx)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", file.Name(), path, err)
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return verror.Make(errOperationFailed, ctx)
	}
	if err := packages.SaveMediaInfo(path, mediaInfo); err != nil {
		vlog.Errorf("packages.SaveMediaInfo(%v, %v) failed: %v", path, mediaInfo, err)
		if err := os.Remove(path); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", path, err)
		}
		return verror.Make(errOperationFailed, ctx)
	}
	return nil
}

func DownloadURL(ctx *context.T, von string) (string, int64, error) {
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	url, ttl, err := repository.BinaryClient(von).DownloadURL(ctx)
	if err != nil {
		vlog.Errorf("DownloadURL() failed: %v", err)
		return "", 0, err
	}
	return url, ttl, nil
}

func uploadPartAttempt(ctx *context.T, r io.ReadSeeker, client repository.BinaryClientStub, part int, size int64) (bool, error) {
	ctx, cancel := ctx.WithCancel()
	defer cancel()

	offset := int64(part * partSize)
	if _, err := r.Seek(offset, 0); err != nil {
		vlog.Errorf("Seek(%v, 0) failed: %v", offset, err)
		return false, nil
	}
	stream, err := client.Upload(ctx, int32(part))
	if err != nil {
		vlog.Errorf("Upload(%v) failed: %v", part, err)
		return false, nil
	}
	bufferSize := partSize
	if remaining := size - offset; remaining < int64(bufferSize) {
		bufferSize = int(remaining)
	}
	buffer := make([]byte, bufferSize)

	nread := 0
	for nread < len(buffer) {
		n, err := r.Read(buffer[nread:])
		nread += n
		if err != nil && (err != io.EOF || nread < len(buffer)) {
			vlog.Errorf("Read() failed: %v", err)
			return false, nil
		}
	}
	sender := stream.SendStream()
	for from := 0; from < len(buffer); from += subpartSize {
		to := from + subpartSize
		if to > len(buffer) {
			to = len(buffer)
		}
		if err := sender.Send(buffer[from:to]); err != nil {
			vlog.Errorf("Send() failed: %v", err)
			return false, nil
		}
	}
	if err := sender.Close(); err != nil {
		vlog.Errorf("Close() failed: %v", err)
		parts, _, statErr := client.Stat(ctx)
		if statErr != nil {
			vlog.Errorf("Stat() failed: %v", statErr)
			if deleteErr := client.Delete(ctx); err != nil {
				vlog.Errorf("Delete() failed: %v", deleteErr)
			}
			return false, err
		}
		if parts[part].Checksum == binary.MissingChecksum {
			return false, nil
		}
	}
	if err := stream.Finish(); err != nil {
		vlog.Errorf("Finish() failed: %v", err)
		parts, _, statErr := client.Stat(ctx)
		if statErr != nil {
			vlog.Errorf("Stat() failed: %v", statErr)
			if deleteErr := client.Delete(ctx); err != nil {
				vlog.Errorf("Delete() failed: %v", deleteErr)
			}
			return false, err
		}
		if parts[part].Checksum == binary.MissingChecksum {
			return false, nil
		}
	}
	return true, nil
}

func uploadPart(ctx *context.T, r io.ReadSeeker, client repository.BinaryClientStub, part int, size int64) error {
	for i := 0; i < nAttempts; i++ {
		if success, err := uploadPartAttempt(ctx, r, client, part, size); success || err != nil {
			return err
		}
	}
	return verror.Make(errOperationFailed, ctx)
}

func upload(ctx *context.T, r io.ReadSeeker, mediaInfo repository.MediaInfo, von string) error {
	client := repository.BinaryClient(von)
	offset, whence := int64(0), 2
	size, err := r.Seek(offset, whence)
	if err != nil {
		vlog.Errorf("Seek(%v, %v) failed: %v", offset, whence, err)
		return verror.Make(errOperationFailed, ctx)
	}
	nparts := (size-1)/partSize + 1
	if err := client.Create(ctx, int32(nparts), mediaInfo); err != nil {
		vlog.Errorf("Create() failed: %v", err)
		return err
	}
	for i := 0; int64(i) < nparts; i++ {
		if err := uploadPart(ctx, r, client, i, size); err != nil {
			return err
		}
	}
	return nil
}

func Upload(ctx *context.T, von string, data []byte, mediaInfo repository.MediaInfo) error {
	buffer := bytes.NewReader(data)
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	return upload(ctx, buffer, mediaInfo, von)
}

func UploadFromFile(ctx *context.T, von, path string) error {
	file, err := os.Open(path)
	defer file.Close()
	if err != nil {
		vlog.Errorf("Open(%v) failed: %v", err)
		return verror.Make(errOperationFailed, ctx)
	}
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	mediaInfo := packages.MediaInfoForFileName(path)
	return upload(ctx, file, mediaInfo, von)
}

func UploadFromDir(ctx *context.T, von, sourceDir string) error {
	dir, err := ioutil.TempDir("", "create-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	zipfile := filepath.Join(dir, "file.zip")
	if err := packages.CreateZip(zipfile, sourceDir); err != nil {
		return err
	}
	return UploadFromFile(ctx, von, zipfile)
}

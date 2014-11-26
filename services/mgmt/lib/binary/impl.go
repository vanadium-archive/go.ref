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
	"time"

	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/services/mgmt/binary"
	"veyron.io/veyron/veyron2/services/mgmt/repository"
	"veyron.io/veyron/veyron2/verror"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/services/mgmt/lib/packages"
)

var (
	errOperationFailed = verror.Internalf("operation failed")
	errNotExist        = verror.NoExistf("binary does not exist")
)

const (
	nAttempts   = 2
	partSize    = 1 << 22
	subpartSize = 1 << 12
)

func Delete(ctx context.T, name string) error {
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	if err := repository.BinaryClient(name).Delete(ctx); err != nil {
		vlog.Errorf("Delete() failed: %v", err)
		return err
	}
	return nil
}

func download(ctx context.T, w io.WriteSeeker, von string) (repository.MediaInfo, error) {
	client := repository.BinaryClient(von)
	parts, mediaInfo, err := client.Stat(ctx)
	if err != nil {
		vlog.Errorf("Stat() failed: %v", err)
		return repository.MediaInfo{}, err
	}
	for _, part := range parts {
		if part.Checksum == binary.MissingChecksum {
			return repository.MediaInfo{}, errNotExist
		}
	}
	offset, whence := int64(0), 0
	for i, part := range parts {
		success := false
	download:
		for j := 0; !success && j < nAttempts; j++ {
			if _, err := w.Seek(offset, whence); err != nil {
				vlog.Errorf("Seek(%v, %v) failed: %v", offset, whence, err)
				continue
			}
			stream, err := client.Download(ctx, int32(i))
			if err != nil {
				vlog.Errorf("Download(%v) failed: %v", i, err)
				continue
			}
			h, nreceived := md5.New(), 0
			rStream := stream.RecvStream()
			for rStream.Advance() {
				bytes := rStream.Value()
				if _, err := w.Write(bytes); err != nil {
					vlog.Errorf("Write() failed: %v", err)
					stream.Cancel()
					continue download
				}
				h.Write(bytes)
				nreceived += len(bytes)
			}

			if err := rStream.Err(); err != nil {
				vlog.Errorf("Advance() failed: %v", err)
				stream.Cancel()
				continue download

			}
			if err := stream.Finish(); err != nil {
				vlog.Errorf("Finish() failed: %v", err)
				continue
			}
			if expected, got := part.Checksum, hex.EncodeToString(h.Sum(nil)); expected != got {
				vlog.Errorf("Unexpected checksum: expected %v, got %v", expected, got)
				continue
			}
			if expected, got := part.Size, int64(nreceived); expected != got {
				vlog.Errorf("Unexpected size: expected %v, got %v", expected, got)
				continue
			}
			success = true
		}
		if !success {
			return repository.MediaInfo{}, errOperationFailed
		}
		offset += part.Size
	}
	return mediaInfo, nil
}

func Download(ctx context.T, von string) ([]byte, repository.MediaInfo, error) {
	dir, prefix := "", ""
	file, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		vlog.Errorf("TempFile(%v, %v) failed: %v", dir, prefix, err)
		return nil, repository.MediaInfo{}, errOperationFailed
	}
	defer os.Remove(file.Name())
	defer file.Close()
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	mediaInfo, err := download(ctx, file, von)
	if err != nil {
		return nil, repository.MediaInfo{}, errOperationFailed
	}
	bytes, err := ioutil.ReadFile(file.Name())
	if err != nil {
		vlog.Errorf("ReadFile(%v) failed: %v", file.Name(), err)
		return nil, repository.MediaInfo{}, errOperationFailed
	}
	return bytes, mediaInfo, nil
}

func DownloadToFile(ctx context.T, von, path string) error {
	dir, prefix := "", ""
	file, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		vlog.Errorf("TempFile(%v, %v) failed: %v", dir, prefix, err)
		return errOperationFailed
	}
	defer file.Close()
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	mediaInfo, err := download(ctx, file, von)
	if err != nil {
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return errOperationFailed
	}
	perm := os.FileMode(0600)
	if err := file.Chmod(perm); err != nil {
		vlog.Errorf("Chmod(%v) failed: %v", perm, err)
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return errOperationFailed
	}
	if err := os.Rename(file.Name(), path); err != nil {
		vlog.Errorf("Rename(%v, %v) failed: %v", file.Name(), path, err)
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return errOperationFailed
	}
	if err := packages.SaveMediaInfo(path, mediaInfo); err != nil {
		vlog.Errorf("packages.SaveMediaInfo(%v, %v) failed: %v", path, mediaInfo, err)
		if err := os.Remove(path); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", path, err)
		}
		return errOperationFailed
	}
	return nil
}

func upload(ctx context.T, r io.ReadSeeker, mediaInfo repository.MediaInfo, von string) error {
	client := repository.BinaryClient(von)
	offset, whence := int64(0), 2
	size, err := r.Seek(offset, whence)
	if err != nil {
		vlog.Errorf("Seek(%v, %v) failed: %v", offset, whence, err)
		return errOperationFailed
	}
	nparts := (size-1)/partSize + 1
	if err := client.Create(ctx, int32(nparts), mediaInfo); err != nil {
		vlog.Errorf("Create() failed: %v", err)
		return err
	}
	for i := 0; int64(i) < nparts; i++ {
		success := false
	upload:
		for j := 0; !success && j < nAttempts; j++ {
			offset, whence := int64(i*partSize), 0
			if _, err := r.Seek(offset, whence); err != nil {
				vlog.Errorf("Seek(%v, %v) failed: %v", offset, whence, err)
				continue
			}
			stream, err := client.Upload(ctx, int32(i))
			if err != nil {
				vlog.Errorf("Upload(%v) failed: %v", i, err)
				continue
			}
			buffer := make([]byte, partSize)
			if int64(i+1) == nparts {
				buffer = buffer[:(size % partSize)]
			}
			nread := 0
			for nread < len(buffer) {
				n, err := r.Read(buffer[nread:])
				nread += n
				if err != nil && (err != io.EOF || nread < len(buffer)) {
					vlog.Errorf("Read() failed: %v", err)
					stream.Cancel()
					continue upload
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
					stream.Cancel()
					continue upload
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
					return err
				}
				if parts[i].Checksum == binary.MissingChecksum {
					stream.Cancel()
					continue
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
					return err
				}
				if parts[i].Checksum == binary.MissingChecksum {
					continue
				}
			}
			success = true
		}
		if !success {
			return errOperationFailed
		}
	}
	return nil
}

func Upload(ctx context.T, von string, data []byte, mediaInfo repository.MediaInfo) error {
	buffer := bytes.NewReader(data)
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	return upload(ctx, buffer, mediaInfo, von)
}

func UploadFromFile(ctx context.T, von, path string) error {
	file, err := os.Open(path)
	defer file.Close()
	if err != nil {
		vlog.Errorf("Open(%v) failed: %v", err)
		return errOperationFailed
	}
	ctx, cancel := ctx.WithTimeout(time.Minute)
	defer cancel()
	mediaInfo := packages.MediaInfoForFileName(path)
	return upload(ctx, file, mediaInfo, von)
}

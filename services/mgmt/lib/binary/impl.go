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

	"veyron2/rt"
	"veyron2/services/mgmt/binary"
	"veyron2/services/mgmt/repository"
	"veyron2/verror"
	"veyron2/vlog"
)

var (
	errOperationFailed = verror.Internalf("operation failed")
	errNotExist        = verror.NotFoundf("binary does not exist")
)

const (
	nAttempts   = 2
	partSize    = 1 << 22
	subpartSize = 1 << 12
)

func Delete(name string) error {
	client, err := repository.BindBinary(name)
	if err != nil {
		vlog.Errorf("BindBinary(%v) failed: %v", name, err)
		return err
	}
	if err := client.Delete(rt.R().NewContext()); err != nil {
		vlog.Errorf("Delete() failed: %v", err)
		return err
	}
	return nil
}

func download(w io.WriteSeeker, von string) error {
	client, err := repository.BindBinary(von)
	if err != nil {
		vlog.Errorf("BindBinary(%v) failed: %v", von, err)
		return err
	}
	parts, err := client.Stat(rt.R().NewContext())
	if err != nil {
		vlog.Errorf("Stat() failed: %v", err)
		return err
	}
	for _, part := range parts {
		if part.Checksum == binary.MissingChecksum {
			return errNotExist
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
			stream, err := client.Download(rt.R().NewContext(), int32(i))
			if err != nil {
				vlog.Errorf("Download(%v) failed: %v", i, err)
				continue
			}
			h, nreceived := md5.New(), 0
			for {
				bytes, err := stream.Recv()
				if err != nil {
					if err != io.EOF {
						vlog.Errorf("Recv() failed: %v", err)
						stream.Cancel()
						continue download
					}
					break
				}
				if _, err := w.Write(bytes); err != nil {
					vlog.Errorf("Write() failed: %v", err)
					stream.Cancel()
					continue download
				}
				h.Write(bytes)
				nreceived += len(bytes)
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
			return errOperationFailed
		}
		offset += part.Size
	}
	return nil
}

func Download(von string) ([]byte, error) {
	dir, prefix := "", ""
	file, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		vlog.Errorf("TempFile(%v, %v) failed: %v", dir, prefix, err)
		return nil, errOperationFailed
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := download(file, von); err != nil {
		return nil, errOperationFailed
	}
	bytes, err := ioutil.ReadFile(file.Name())
	if err != nil {
		vlog.Errorf("ReadFile(%v) failed: %v", file.Name(), err)
		return nil, errOperationFailed
	}
	return bytes, nil
}

func DownloadToFile(von, path string) error {
	dir, prefix := "", ""
	file, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		vlog.Errorf("TempFile(%v, %v) failed: %v", dir, prefix, err)
		return errOperationFailed
	}
	defer file.Close()
	if err := download(file, von); err != nil {
		if err := os.Remove(file.Name()); err != nil {
			vlog.Errorf("Remove(%v) failed: %v", file.Name(), err)
		}
		return errOperationFailed
	}
	perm := os.FileMode(0700)
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
	return nil
}

func upload(r io.ReadSeeker, von string) error {
	client, err := repository.BindBinary(von)
	if err != nil {
		vlog.Errorf("BindBinary(%v) failed: %v", von, err)
		return err
	}
	offset, whence := int64(0), 2
	size, err := r.Seek(offset, whence)
	if err != nil {
		vlog.Errorf("Seek(%v, %v) failed: %v", offset, whence, err)
		return errOperationFailed
	}
	nparts := (size-1)/partSize + 1
	if err := client.Create(rt.R().NewContext(), int32(nparts)); err != nil {
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
			stream, err := client.Upload(rt.R().NewContext(), int32(i))
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
			for from := 0; from < len(buffer); from += subpartSize {
				to := from + subpartSize
				if to > len(buffer) {
					to = len(buffer)
				}
				if err := stream.Send(buffer[from:to]); err != nil {
					vlog.Errorf("Send() failed: %v", err)
					stream.Cancel()
					continue upload
				}
			}
			if err := stream.CloseSend(); err != nil {
				vlog.Errorf("CloseSend() failed: %v", err)
				parts, statErr := client.Stat(rt.R().NewContext())
				if statErr != nil {
					vlog.Errorf("Stat() failed: %v", statErr)
					if deleteErr := client.Delete(rt.R().NewContext()); err != nil {
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
				parts, statErr := client.Stat(rt.R().NewContext())
				if statErr != nil {
					vlog.Errorf("Stat() failed: %v", statErr)
					if deleteErr := client.Delete(rt.R().NewContext()); err != nil {
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

func Upload(von string, data []byte) error {
	buffer := bytes.NewReader(data)
	return upload(buffer, von)
}

func UploadFromFile(von, path string) error {
	file, err := os.Open(path)
	defer file.Close()
	if err != nil {
		vlog.Errorf("Open(%v) failed: %v", err)
		return errOperationFailed
	}
	return upload(file, von)
}

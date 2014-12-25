// Package packages provides functionality to install ZIP and TAR packages.
package packages

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"v.io/veyron/veyron2/services/mgmt/repository"
)

const defaultType = "application/octet-stream"

var typemap = map[string]repository.MediaInfo{
	".zip":     repository.MediaInfo{Type: "application/zip"},
	".tar":     repository.MediaInfo{Type: "application/x-tar"},
	".tgz":     repository.MediaInfo{Type: "application/x-tar", Encoding: "gzip"},
	".tar.gz":  repository.MediaInfo{Type: "application/x-tar", Encoding: "gzip"},
	".tbz2":    repository.MediaInfo{Type: "application/x-tar", Encoding: "bzip2"},
	".tb2":     repository.MediaInfo{Type: "application/x-tar", Encoding: "bzip2"},
	".tbz":     repository.MediaInfo{Type: "application/x-tar", Encoding: "bzip2"},
	".tar.bz2": repository.MediaInfo{Type: "application/x-tar", Encoding: "bzip2"},
}

// MediaInfoForFileName returns the MediaInfo based on the file's extension.
func MediaInfoForFileName(fileName string) repository.MediaInfo {
	fileName = strings.ToLower(fileName)
	for k, v := range typemap {
		if strings.HasSuffix(fileName, k) {
			return v
		}
	}
	return repository.MediaInfo{Type: defaultType}
}

// Install installs a package in the given directory. If the package is a TAR or
// ZIP archive, its content is extracted in the destination directory.
// Otherwise, the package file itself is copied to the destination directory.
func Install(pkgFile, dir string) error {
	mediaInfo, err := LoadMediaInfo(pkgFile)
	if err != nil {
		return err
	}
	switch mediaInfo.Type {
	case "application/x-tar":
		return extractTar(pkgFile, mediaInfo, dir)
	case "application/zip":
		return extractZip(pkgFile, dir)
	default:
		return fmt.Errorf("unsupported media type: %v", mediaInfo.Type)
	}
}

// LoadMediaInfo returns the MediaInfo for the given package file.
func LoadMediaInfo(pkgFile string) (repository.MediaInfo, error) {
	jInfo, err := ioutil.ReadFile(pkgFile + ".__info")
	if err != nil {
		return repository.MediaInfo{}, err
	}
	var info repository.MediaInfo
	if err := json.Unmarshal(jInfo, &info); err != nil {
		return repository.MediaInfo{}, err
	}
	return info, nil
}

// SaveMediaInfo saves the media info for a package.
func SaveMediaInfo(pkgFile string, mediaInfo repository.MediaInfo) error {
	jInfo, err := json.Marshal(mediaInfo)
	if err != nil {
		return err
	}
	infoFile := pkgFile + ".__info"
	if err := ioutil.WriteFile(infoFile, jInfo, os.FileMode(0600)); err != nil {
		return err
	}
	return nil
}

// CreateZip creates a package from the files in the source directory. The
// created package is a Zip file.
func CreateZip(zipFile, sourceDir string) error {
	z, err := os.OpenFile(zipFile, os.O_CREATE|os.O_WRONLY, os.FileMode(0644))
	if err != nil {
		return err
	}
	defer z.Close()
	w := zip.NewWriter(z)
	if err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if sourceDir == path {
			return nil
		}
		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		fh.Name, _ = filepath.Rel(sourceDir, path)
		hdr, err := w.CreateHeader(fh)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			content, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}
			if _, err = hdr.Write(content); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := SaveMediaInfo(zipFile, repository.MediaInfo{Type: "application/zip"}); err != nil {
		return err
	}
	return nil
}

func extractZip(zipFile, installDir string) error {
	zr, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	for _, file := range zr.File {
		fi := file.FileInfo()
		name := filepath.Join(installDir, file.Name)
		if !strings.HasPrefix(name, installDir) {
			return fmt.Errorf("failed to extract file %q outside of install directory", file.Name)
		}
		if fi.IsDir() {
			if err := os.MkdirAll(name, os.FileMode(fi.Mode()&0700)); err != nil && !os.IsExist(err) {
				return err
			}
			continue
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		parentName := filepath.Dir(name)
		if err := os.MkdirAll(parentName, os.FileMode(0700)); err != nil {
			return err
		}
		out, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY, os.FileMode(fi.Mode()&0700))
		if err != nil {
			in.Close()
			return err
		}
		nbytes, err := io.Copy(out, in)
		in.Close()
		out.Close()
		if err != nil {
			return err
		}
		if nbytes != fi.Size() {
			return fmt.Errorf("file size doesn't match for %q: %d != %d", fi.Name(), nbytes, fi.Size())
		}
	}
	return nil
}

func extractTar(pkgFile string, mediaInfo repository.MediaInfo, installDir string) error {
	f, err := os.Open(pkgFile)
	if err != nil {
		return err
	}
	defer f.Close()

	var reader io.Reader
	switch enc := mediaInfo.Encoding; enc {
	case "":
		reader = f
	case "gzip":
		var err error
		if reader, err = gzip.NewReader(f); err != nil {
			return err
		}
	case "bzip2":
		reader = bzip2.NewReader(f)
	default:
		return fmt.Errorf("unsupported encoding: %q", enc)
	}

	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Join(installDir, hdr.Name)
		if !strings.HasPrefix(name, installDir) {
			return fmt.Errorf("failed to extract file %q outside of install directory", hdr.Name)
		}
		// Regular file
		if hdr.Typeflag == tar.TypeReg {
			out, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode&0700))
			if err != nil {
				return err
			}
			nbytes, err := io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
			if nbytes != hdr.Size {
				return fmt.Errorf("file size doesn't match for %q: %d != %d", hdr.Name, nbytes, hdr.Size)
			}
			continue
		}
		// Directory
		if hdr.Typeflag == tar.TypeDir {
			if err := os.Mkdir(name, os.FileMode(hdr.Mode&0700)); err != nil && !os.IsExist(err) {
				return err
			}
			continue
		}
		// Skip unsupported types
		// TODO(rthellend): Consider adding support for Symlink.
	}
}

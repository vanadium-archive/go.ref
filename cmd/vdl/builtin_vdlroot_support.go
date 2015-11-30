// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractTarFile(basePath string, reader *tar.Reader) error {
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		// If it's a directory, ignore it. We use MkdirAll on all files anyway.
		if header.FileInfo().IsDir() {
			continue
		}
		fullPath := filepath.Join(basePath, header.Name)
		if err := os.MkdirAll(filepath.Dir(fullPath), os.FileMode(0755)); err != nil {
			return err
		}
		f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
		if err != nil {
			return err
		}
		n, err := io.Copy(f, reader)
		if err != nil {
			return err
		}
		if n != header.Size {
			return fmt.Errorf("while reading %s from archive, wrote %d bytes but was expecting to write %d", header.Name, header.Size, n)
		}
	}
}

func extractVDLRootData(destDir string) error {
	r := strings.NewReader(builtinVDLRootData)
	gzipReader, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tarReader := tar.NewReader(gzipReader)
	return extractTarFile(destDir, tarReader)
}

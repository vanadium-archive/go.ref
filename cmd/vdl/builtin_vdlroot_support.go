// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"v.io/x/ref/lib/vdl/build"
	"v.io/x/ref/lib/vdl/vdlutil"
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

func extractBuiltinVdlroot(destDir string) error {
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(builtinVdlrootData))
	gzipReader, err := gzip.NewReader(decoder)
	if err != nil {
		return err
	}
	tarReader := tar.NewReader(gzipReader)
	return extractTarFile(destDir, tarReader)
}

// maybeExtractBuiltinVdlroot checks to see if VDLROOT is set or can be
// determined from V23_ROOT. If not, the builtin root VDL definitions are
// extracted to a new temporary directory and the VDLROOT environment variable
// is set. cleanupFunc should be called unconditionally (even if there is
// a non-empty set of errors returned).
func maybeExtractBuiltinVdlroot() (func() error, *vdlutil.Errors) {
	noopCleanup := func() error { return nil }
	if !flagBuiltinVdlroot {
		return noopCleanup, vdlutil.NewErrors(-1)
	}
	errs := vdlutil.NewErrors(-1)
	build.VdlRootDir(errs)
	if errs.IsEmpty() {
		// No errors? We have a vdlroot already and we don't need to do
		// anything.
		return noopCleanup, errs
	}
	errs.Reset()
	// Otherwise, we don't have a VDL rootdir.
	dir, err := ioutil.TempDir("", "vdlroot-")
	if err != nil {
		errs.Errorf("TempDir failed, couldn't create temporary VDLROOT: %v", err)
		return noopCleanup, errs
	}
	removeAllCleanup := func() error {
		return os.RemoveAll(dir)
	}
	// Extract the files to the new VDLROOT
	if err := extractBuiltinVdlroot(dir); err != nil {
		errs.Errorf("Could not extract builtin VDL types: %v", err)
		return removeAllCleanup, errs
	}
	if err := os.Setenv("VDLROOT", dir); err != nil {
		errs.Errorf("Setenv(VDLROOT, %q) failed: %v", dir, err)
		return removeAllCleanup, errs
	}
	vdlutil.Vlog.Printf("set VDLROOT to newly-extracted root at %s pid = %d", dir, os.Getpid())
	return removeAllCleanup, errs
}

// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Code generated by go-bindata.
// sources:
// v.io/v23/vdlroot/math/math.vdl
// v.io/v23/vdlroot/math/vdl.config
// v.io/v23/vdlroot/signature/signature.vdl
// v.io/v23/vdlroot/time/time.vdl
// v.io/v23/vdlroot/time/vdl.config
// v.io/v23/vdlroot/vdltool/config.vdl
// DO NOT EDIT!

package builtin_vdlroot

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bindataRead(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (fi bindataFileInfo) Name() string {
	return fi.name
}
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}
func (fi bindataFileInfo) IsDir() bool {
	return false
}
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _mathMathVdl = []byte("\x1f\x8b\x08\x00\x00\x09\x6e\x88\x00\xff\x94\x8e\xb1\x4e\xc3\x30\x14\x45\xe7\xfa\x2b\xee\x0f\x34\xa5\x69\x15\xb1\x96\xd2\xa1\x12\x62\xa0\xc0\xfe\x92\xbc\x24\x16\x8e\x1d\xd9\xcf\x15\x11\xea\xbf\xe3\x64\x40\x19\x90\x10\xe3\xb1\x7c\xde\x3d\x9b\x0d\x8e\x6e\x18\xbd\x6e\x3b\x41\x7e\xb7\x2d\xf0\xda\x31\xde\xc9\x52\xad\x63\x8f\x43\x94\xce\xf9\x90\xe1\x60\x0c\xe6\x4f\x01\x9e\x03\xfb\x2b\xd7\x99\x4a\xf2\x5b\x60\xb8\x06\xd2\xe9\x80\xe0\xa2\xaf\x18\x95\xab\x19\x09\x5b\x77\x65\x6f\xb9\x46\x39\x82\xf0\x70\x79\x5c\x07\x19\x0d\x4f\x96\xd1\x15\xdb\x64\x4a\x47\x82\x8a\x2c\x4a\x46\xe3\xa2\xad\xa1\x6d\x7a\x64\x3c\x9d\x8f\xa7\xe7\xcb\x09\x8d\x36\x9c\x29\x35\x50\xf5\x41\x2d\xa3\x27\xe9\x94\x9a\x9b\xfb\xc1\xf0\x67\xb1\x9f\x86\x28\x4d\xce\x08\x1b\xfb\x92\xfd\x8c\x2e\xa4\xe5\x54\xb6\xcb\xd7\xa5\x96\x14\x4d\x06\x34\x0d\xf4\xd4\x6a\x4b\x7e\xc4\x40\x5e\x42\xa6\x64\x1c\x78\x71\x2f\x88\x8f\x95\xe0\x4b\xad\x5e\x26\xa5\x31\x8e\x64\x97\xab\xd5\x39\x79\x3f\x74\x5b\x46\x6c\xf3\xfb\x3f\x2b\x8a\xfd\x3f\x2a\xa6\x83\xbf\x65\x14\xfb\x65\x46\xa2\x9b\xfa\x0e\x00\x00\xff\xff\xce\xca\xd1\x3a\xbf\x01\x00\x00")

func mathMathVdlBytes() ([]byte, error) {
	return bindataRead(
		_mathMathVdl,
		"math/math.vdl",
	)
}

func mathMathVdl() (*asset, error) {
	bytes, err := mathMathVdlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "math/math.vdl", size: 0, mode: os.FileMode(420), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _mathVdlConfig = []byte("\x1f\x8b\x08\x00\x00\x09\x6e\x88\x00\xff\x8c\x8d\x41\x4b\xc4\x30\x10\x85\xcf\xed\xaf\x78\xf4\x2c\x5d\x14\x11\x09\x78\xda\x83\x07\x71\x4f\x15\xc1\x5b\x4c\xa6\xed\x60\x9a\xa9\xd9\x74\x51\x64\xff\xbb\x49\xa3\x7b\xee\x21\x13\xf8\xde\x9b\x6f\x76\x3b\x74\x23\xe1\x64\x5d\x6b\xc4\xf7\x3c\xa0\x67\x47\xe8\x25\x20\x26\x3e\xe9\x38\x62\xd6\xe6\x43\x0f\x84\x52\x58\x02\x1d\xc1\x96\x25\x65\x6c\xe0\xd3\x3c\x11\xe2\xf7\x4c\xc7\x3a\xd9\xd8\x83\xb4\x19\x31\x90\xa7\xa0\x23\x59\x38\xed\x87\x25\xed\xb7\xf5\xdf\x85\x87\x7c\x2e\x8a\xb8\x76\xbf\x82\x9f\xba\x7a\x14\x85\xf4\x55\xaf\x1c\xa8\x93\xc3\xea\xec\xb2\xb2\xe0\xaa\xd9\xcb\x34\x3b\xfa\xba\xbb\x6d\x14\x0a\xaa\x9e\xd8\x5b\x85\xc3\x32\xbd\x53\xb8\x5a\x49\xde\x50\x68\xcc\xa5\x5b\xf0\x1b\x85\xac\x7f\x16\x9b\xd2\x17\xcf\x9f\x0b\x9d\xd7\xa4\xcc\x7f\xf7\xf5\xcd\x7d\xa3\x36\xba\x73\x77\x8b\x3c\x8f\xf4\xce\xf5\x6f\x00\x00\x00\xff\xff\x03\xf0\x4f\xb5\x68\x01\x00\x00")

func mathVdlConfigBytes() ([]byte, error) {
	return bindataRead(
		_mathVdlConfig,
		"math/vdl.config",
	)
}

func mathVdlConfig() (*asset, error) {
	bytes, err := mathVdlConfigBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "math/vdl.config", size: 0, mode: os.FileMode(420), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _signatureSignatureVdl = []byte("\x1f\x8b\x08\x00\x00\x09\x6e\x88\x00\xff\x9c\x53\x41\x8f\xd3\x3c\x14\x3c\xd7\xbf\x62\x8e\xdf\x77\x20\x0b\x48\x9c\x51\xd9\xed\xa1\x12\x74\x2b\xb5\x70\x59\xed\xc1\x89\x5f\x12\xb3\x89\x1d\xd9\xce\x4a\x11\xda\xff\xce\xb3\x63\xd2\xd2\xa2\x15\xa2\x27\xfb\x79\x66\xde\xcc\xb4\xbd\xb9\xc1\xad\x1d\x26\xa7\x9b\x36\xe0\xfd\xdb\x77\x1f\x70\x6c\x09\xdf\xa4\x91\x4a\x8f\x3d\xd6\x63\x68\xad\xf3\x05\xd6\x5d\x87\x04\xf2\x70\xe4\xc9\x3d\x93\x2a\x04\x93\xbf\x7a\x82\xad\x11\x5a\xed\xe1\xed\xe8\x2a\x42\x65\x15\x81\xaf\x8d\x7d\x26\x67\x48\xa1\x9c\x20\xf1\xe9\x70\xf7\xc6\x87\xa9\xa3\xc8\xea\x74\x45\x86\x99\xa1\x95\x01\x95\x34\x28\x09\xb5\x1d\x8d\x82\x36\x3c\x24\x7c\xde\xde\x6e\x76\x87\x0d\x6a\xdd\x51\x21\x22\x65\x2f\xab\x27\xd9\x10\xbc\x6e\x8c\x0c\xa3\x23\x28\xaa\xb5\x21\x8f\x30\x0d\x14\x5d\x0d\xd1\x98\x09\xda\x34\xac\x12\xc8\xd5\x92\xcd\x48\xd6\xec\x89\x43\xa8\x13\xd3\x17\x62\xb8\x54\x4b\x3b\xb6\x0b\x4d\x91\xaf\x9c\x2e\xa3\x7a\x7b\xbe\x93\xa3\xb2\xdb\x45\xbe\x10\x71\xf9\x19\xcf\x07\x37\x56\x01\x3f\xc4\x6a\x27\x7b\x02\x7f\x78\xc2\x8e\xc4\x6a\xff\xd4\xec\x65\x68\x97\xfb\x9d\xad\x70\xfe\xbe\xe9\x4b\x52\x1e\x78\x78\x4c\x27\x80\xfd\xec\x2c\xfc\x40\x95\x96\x1d\xac\x53\x14\x81\x85\x58\x7d\x49\x71\x3c\x23\xe7\x53\x44\xde\xc7\xe7\xb9\xea\x9c\xd6\xf0\xfe\x42\xbc\xa4\x5c\xb3\xe2\xeb\x99\x28\x62\x14\xa9\xab\x70\x33\xf9\x9f\x83\xcd\x0e\xb2\xd3\xbf\xac\x35\x67\xc8\x06\x32\xf7\xda\x01\xae\x76\x9e\x26\x5b\xb3\x76\x8d\x8f\x93\x87\x47\x3e\x21\x7d\xbb\xc3\x18\x20\x5d\x33\xf6\xfc\x33\xf1\x62\x75\x3f\x86\x0c\x5a\x30\x3c\xba\x00\x6d\xcd\x21\x38\x92\x3d\xf0\x31\x62\x4e\x42\x7e\x1e\xff\x67\x87\xa0\xad\x91\xdd\xff\x49\x30\x83\x17\x6c\x16\xfc\x03\xf8\x28\x67\x7f\x71\xbb\x34\x13\x4e\x2d\x05\x7e\xc9\xbd\x45\x99\xd7\x4a\xe3\xab\x69\x3a\x5a\x0c\xe7\xca\x22\xed\xa2\xaf\xdf\xaa\xfa\x75\x39\x46\x70\x64\xd8\xf2\x3b\x31\x98\x37\xa6\x51\xfa\x47\x9f\xab\xbe\x88\x9f\x01\x00\x00\xff\xff\x7a\xf5\x7c\x0e\x29\x04\x00\x00")

func signatureSignatureVdlBytes() ([]byte, error) {
	return bindataRead(
		_signatureSignatureVdl,
		"signature/signature.vdl",
	)
}

func signatureSignatureVdl() (*asset, error) {
	bytes, err := signatureSignatureVdlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "signature/signature.vdl", size: 0, mode: os.FileMode(420), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _timeTimeVdl = []byte("\x1f\x8b\x08\x00\x00\x09\x6e\x88\x00\xff\x94\x58\x5d\x73\xdb\xba\x11\x7d\xb6\x7f\x05\xc6\x7d\xb8\xc9\x54\x96\x68\xf7\x36\xad\x93\x27\xd7\xf9\x68\x3a\x69\x92\xa9\x9d\x76\xa6\x2f\x1a\x88\x84\x44\x8c\x49\x80\x05\x40\xc9\x4a\x27\xff\xbd\x67\x17\x00\x49\x29\xae\xeb\x66\x7c\x1f\x24\x61\x17\xfb\x71\xce\xd9\xc5\x5d\x2c\xc4\x8d\xed\xf6\x4e\x6f\xea\x20\x2e\x8b\x8b\xdf\x8b\xbb\x5a\x89\xbf\x4b\x23\x2b\xdd\xb7\xe2\xba\x0f\xb5\x75\x7e\x2e\xae\x9b\x46\xf0\x21\x2f\x9c\xf2\xca\x6d\x55\x35\x3f\x85\xf1\x37\xaf\x84\x5d\x8b\x50\x6b\x2f\xbc\xed\x5d\xa9\x44\x69\x2b\x25\xf0\x71\x63\xb7\xca\x19\x55\x89\xd5\x5e\x48\xf1\xa7\xdb\xb7\xe7\x3e\xec\x1b\x45\x56\x8d\x2e\x95\x81\x65\xa8\x65\x10\xa5\x34\x62\xa5\xc4\xda\xf6\xa6\x12\xda\xe0\x4b\x25\x3e\x7d\xbc\x79\xf7\xf9\xf6\x9d\x58\xeb\x46\xcd\x4f\xc9\xe4\xab\x2c\xef\xe5\x06\x26\xba\x55\xa2\x52\x6b\x6d\x14\x6e\x0c\xd2\x54\xd2\x55\x88\xa9\xa3\xb0\x4c\x90\x41\x5b\xe3\x29\x24\xb9\xf2\xb6\xe9\x83\x12\x38\x82\xdf\x1b\xfc\xb2\x8d\xe6\x9e\x22\x27\x9f\x94\xea\xb1\x65\xa5\x7c\xe9\xf4\x8a\xc2\x56\x8d\xdd\x09\xe9\xe8\xcc\xbf\x7a\xed\xf0\x55\xb0\xa2\x73\x76\xab\x91\xe0\x0e\x5f\x90\x8f\xd2\xb6\x1d\x4c\x57\xba\xd1\x61\x0f\x9b\xb0\x53\xca\x88\x4a\xaf\xd7\xca\xc1\x2b\x9d\xdf\x38\xd9\xb6\xda\x6c\x84\x32\x5b\xed\xac\x69\xf1\x3d\x4a\x2a\x3e\x28\xa3\x9c\x0c\xf0\xcb\x25\x5b\x5b\x47\x0e\x47\xd3\xe9\x71\x11\xf6\x9d\x2e\x65\xd3\xec\x87\x08\x64\x1f\x6c\x8b\xab\x4b\x98\x1b\x94\xda\x73\xfc\xda\x20\x48\xc3\xc9\x92\xb7\xa3\xf4\x66\x74\x8b\xf0\xba\xed\x1a\xe5\x84\xae\x74\xf2\xd0\x7b\xd4\x76\x7e\xda\x4d\x8a\xcc\x55\xff\x5b\x36\xa7\xf0\x2b\xc4\xea\xb9\x9c\x5c\x45\xea\xb1\xe4\xfc\xd1\x4e\x4e\x23\x58\xc4\x88\xc4\x6e\x6d\x9b\x50\x41\x40\xf0\xbd\xf2\xaf\xc9\xd9\xc5\x4b\xae\xb8\x92\x2e\xd4\xbf\x00\x47\xb0\xa1\x53\xce\xc6\xe0\xc8\x9f\xb1\x01\x21\x6f\xfa\x46\x3a\x86\x17\xfe\xd5\x21\x74\xaf\x17\x0b\x65\xe6\x3b\x7d\xaf\x3b\x55\x69\x39\xb7\x6e\xb3\xa0\x4f\x8b\x4f\x4a\x76\x4b\xaf\x50\x80\xea\x39\xc7\x6f\xac\x75\x95\x36\x14\xec\xf2\x9b\xd1\x54\x34\xd9\x2c\xef\x28\xdb\x67\x58\x7f\x34\x01\x80\xe6\x58\x61\x75\x8d\xea\xeb\x72\x30\xbe\xe4\xe4\x00\x16\x02\xcc\xd8\xc3\x8c\x50\xcf\x85\x67\xdc\x52\x29\xd1\x49\x45\xdf\xfb\x67\x65\x59\x2c\x5f\xec\x51\xb5\x97\xcf\x39\xfb\x97\xbe\xd1\xd2\x2c\xf3\x05\xcf\x31\xf9\x80\x8a\x5b\xf7\xff\x5a\x7d\x75\xb6\x51\x1d\xc0\xb3\x7c\xd4\x3e\xf3\xab\xb5\x3e\x08\xe0\xcd\x3a\x54\x02\x64\xaf\x2d\xb8\x4f\xad\xde\xd5\x0a\xf8\x70\xc4\xa9\x01\xa4\x91\xdd\x32\xe2\xaa\xc7\x67\x02\x91\x2e\xef\x3d\x79\xf3\xda\x94\xc4\x66\xa1\x3a\x5b\xd6\xe2\x05\xfd\xc0\x06\x2f\x67\x02\xb5\x65\x2b\x82\xe8\x82\x9d\x50\xb9\xa5\xe8\x80\x35\x5d\x12\x9c\xc4\x34\xb7\x17\xf9\x43\xb4\x07\x64\xef\xb2\x33\xd1\xca\xce\x13\x81\x7a\xc7\x6c\x43\x78\x35\xfa\xb7\xa3\xb6\x96\x8d\x45\x2c\x33\x8a\xbe\xac\x81\x3c\xfc\x78\xc0\xb1\x19\x93\x03\x66\x76\xe7\x41\x29\x6a\x74\xa4\x9a\xf0\xfd\x2a\x38\x59\x32\xcc\x29\x32\x62\x4d\x1f\x49\x85\x7b\x98\x9a\x8c\x84\x9b\x69\x5c\x74\xcf\x48\xfb\xd6\x22\x02\xaf\x5a\x54\x31\x7f\xa3\x24\x81\x69\xdd\x37\x48\xe0\x66\xa2\x01\x49\x85\xc8\xe1\x50\x24\x0e\xed\x20\x6d\x46\xea\x94\xbe\xd0\x69\x62\xac\x5c\x41\xb8\x13\x6f\xb3\x52\x3e\xcd\x8b\xdb\x2f\xcb\x3f\xbe\x2a\x2e\xfe\xe7\xc1\xdb\xbd\x0f\xaa\x5d\x0e\xea\x42\xdc\x19\xab\x47\xd2\x32\xaa\x76\x67\xa1\x64\x3c\x0e\xe8\xd0\x4e\x87\x5a\xf4\x54\x70\xb4\xc6\xd8\xc8\x78\x28\xa1\x2a\x35\xa5\x3c\x08\x7a\xaa\xda\xe0\x13\x59\x01\x17\x94\x55\xae\x33\x8a\xb3\xa6\x4a\x12\x3c\xd6\xe0\x33\x40\xb2\xd6\x0f\x38\xc7\xa8\x22\x24\xe0\xf0\x77\xe5\xec\x63\x01\x92\xa3\x88\xbe\xa2\x28\x2e\xce\xf9\xef\xae\x28\x5e\xf3\xdf\xbc\xc8\xff\xfe\xc9\x6e\x34\x61\x40\xb1\x11\xf9\xea\x32\x5f\xc4\xc0\x97\xa1\x1f\x6f\xd8\xf3\xd0\x1d\xd7\xd3\x08\x33\x8c\xf5\x07\xa0\x46\xfc\x5a\x14\x82\x14\x40\x94\xfb\x92\x06\x22\xdc\x91\xf2\x89\x58\x07\xcf\x9d\x3c\xf3\xc0\x03\x86\xd4\xd9\x0c\x83\xc3\xf7\x8e\xc0\xc5\xf3\xd5\x58\xd1\x8c\x87\x45\x90\xab\x86\x8b\x64\x54\xa9\xbc\x97\x6e\x4f\xee\x08\x94\x9a\xf4\x0d\xb9\x46\x39\x1e\x87\xa4\x66\xa9\x07\x92\x35\xf1\x08\x2d\xf8\x60\xb9\x2b\x73\x2a\xcf\x4c\xac\xfa\x00\xcc\xef\xe4\xde\xe7\xe9\xfd\xed\xee\x46\x80\x2a\x83\x9b\x8c\x8a\x8d\x6d\xa4\xd9\x30\x1e\xba\xfb\x0d\xf3\x74\xf1\x9b\x24\xa0\xff\xe5\x2a\x1e\x6d\x26\x4a\xae\x38\xeb\x8d\x7e\xe0\xab\xcf\xe2\xb5\x0c\x8b\xb1\x29\x71\x2d\x40\xcb\x03\x79\xe3\x82\x5d\xd0\x98\x61\x99\xa9\x51\xcd\xf8\xd5\xd5\x1f\x8a\xdc\xa0\xc4\xd5\x90\x7a\x1e\x1b\x8e\x6b\xb1\x8f\xa0\x77\x0c\x1e\x49\xbe\x92\x1e\x08\x82\x81\xdb\xcf\x92\x16\x69\x00\xd4\x9b\x5f\x90\xbe\xd8\xca\x46\xc7\xb9\xc8\xa5\x04\x4b\x69\x50\x13\xe3\xd1\x55\x21\xbb\x48\xb2\x81\xe7\x4f\xd2\x04\x93\xe9\x21\x92\x04\xec\x57\x31\xa6\xb7\x09\xbe\xcc\x9b\xfc\xe1\x27\x68\x36\xd0\x2e\x44\x3d\xc1\x7a\x5c\x47\xc2\xce\x46\x42\xf9\xcc\xa8\x19\xd7\x8e\x9c\x3d\xc5\x2a\xbe\x7f\xb8\xcd\x07\xd7\x03\x8c\xff\x3e\x3d\x81\xd9\x6d\x82\xde\x51\x08\x19\x91\x09\x09\x39\x92\xc4\x2b\x87\xf6\x47\x82\xda\x7e\x53\x37\x7b\xf6\xf4\xdb\xc5\xf9\xe5\x55\x21\xb0\x41\x35\x74\x0b\xf5\x08\x1a\x8b\xfe\x6f\x72\xdb\x38\x37\x8f\xb0\x59\xa6\x68\x41\x49\xcb\x45\x1f\x67\x38\x18\x71\x72\x3b\x5c\x1c\x5e\xfd\xca\x7e\x3f\x53\x4a\xc7\xf1\xad\x93\x0a\xc7\x15\x31\x73\x82\x58\x32\x16\x00\xe7\x49\x83\x62\xd8\x7f\xed\x7d\x60\x77\x80\x44\x4a\x0a\xbd\x6f\xa0\xee\xdb\x9c\x4f\xae\x32\x12\xb9\xba\xba\x9a\xa5\xff\xe6\x64\xc5\x96\x1f\x0d\x38\xe8\x5a\x20\xe4\x3b\xc2\x07\x3c\xda\xd9\x28\xfb\xe0\xa6\xf7\x31\x4d\x6b\xd4\x10\x90\x53\x07\x22\xc6\x40\x2f\x0e\x0a\x4f\x7a\x8e\x1b\x39\x4b\xc4\xf9\x9e\xd6\x8b\xc1\xe9\xc4\x15\xa1\x11\x82\x37\x8b\xed\xd1\x1b\x43\x99\xb3\x15\xbb\x6b\x91\x1f\xe0\x1a\x40\x9f\xe4\x99\xa7\x29\xb2\x2d\x90\x41\xac\x21\x6a\xfa\xbb\xcb\xd3\x1f\x8c\xbd\x7f\x60\xef\x7d\xab\x64\xd5\x80\x69\xc7\xc5\xad\xf2\xf7\x3c\x7c\x71\x51\xa7\x62\x40\x33\x9a\xf6\x4e\xf1\xa1\xe1\x4b\xe0\x80\x1c\xaa\x87\x4e\x95\x71\x7b\x14\xb4\x1d\xf9\x3a\x4b\xf4\xd4\x65\x02\x10\x49\x94\xa9\x70\x98\x17\x56\x42\x52\x9c\xf3\x65\xa3\x11\x05\x0f\x3c\x8b\xc4\x89\x91\x43\x30\x51\x48\x27\xb1\x78\xb9\xe7\x02\x61\x27\xa7\x49\xb3\x76\xb6\x15\x67\xc6\xee\xce\xe2\xfc\x46\x3e\x55\x16\xed\xc1\x09\xdc\x12\x5a\xf0\xee\x71\x29\x94\xf8\x81\x42\x78\x24\x83\xc3\x44\x63\x3e\x1c\xdd\x34\xa5\xa4\x7a\x5f\x0c\xb9\x86\x5c\x37\xf4\xdb\x16\xeb\xd1\x2c\x2a\x4b\x5a\x1e\xd0\x9f\x83\x1d\x69\x92\x9a\x3c\x98\x99\x71\xde\xb7\xea\x0d\x55\x3b\x42\x95\x05\x09\xa9\xc1\x5c\x62\x65\x39\x6c\xd3\xec\xb0\x69\xb5\xe4\x54\xe8\xa1\xc3\x1a\xf5\x67\xbb\x53\x94\x21\x3b\xa3\xa2\xd0\x44\x99\x5c\x3e\xbc\x75\x38\x66\x1f\x31\xda\xd9\x40\x72\x0d\xcd\xe3\x65\x49\xf8\x7b\xb5\xa3\x74\xc8\xa1\x26\xd2\xe1\xe7\xb8\xca\x38\xbb\xea\x8f\x53\x3b\x88\x27\x2e\x74\x59\x7c\xc6\x2e\xc5\xfa\xf3\x46\x48\x58\xc8\xcb\x94\x3a\x9a\xf3\x6c\xa0\x03\xbf\x29\xe8\x33\x50\x9f\x9a\xbc\xab\xf1\xaa\xe4\xc3\x50\x3a\x45\x0a\x92\x1f\x72\x83\x97\x61\x0d\x19\x21\x9d\xdd\xd9\x9d\x39\x76\x79\x30\x2a\x0f\x5f\x5c\xa2\xb2\x2a\x8e\x88\x32\x6e\xb4\x84\x57\xac\x02\x9d\xdc\xa4\xdf\x21\xda\xfb\x38\xcd\xe8\x68\x9c\xdd\x09\x2b\xf2\xa8\x22\x3c\x92\xd2\x96\x96\x43\x8f\xaf\x26\x7a\x54\x81\x02\x13\xbd\xcc\x46\x5c\xf8\xcc\x9a\x64\x1b\xd9\x82\x3a\x42\x99\x78\x52\x21\xf7\x96\x04\x1e\xaf\x72\xe9\xb4\x8d\x35\xf8\x29\xca\x44\x58\x0f\x25\x6b\xe2\xbb\x37\xbd\x89\xf9\xf4\xa3\x8d\x6f\xe5\x7d\xdc\x43\xf8\xad\x98\x96\x61\xe4\xca\xc4\xc7\x1b\xa9\xe4\x47\x39\xc6\xa3\xb3\xc0\x67\xae\xe3\x81\xc6\x8c\x1b\x70\xcd\x88\x88\x6f\xdb\xe3\x32\xd3\x09\x04\x33\x5a\x51\xf1\xe8\x52\xde\x82\x87\x76\xc6\x8d\x05\x00\x80\xdc\x0d\xef\xe7\xf8\xaa\x56\x8e\x94\x39\x4f\x31\x06\x56\xfc\x3f\x07\x53\x88\x8c\x3b\xf6\x04\x8f\x71\x4c\x1e\x04\x7d\x30\x2a\xdf\xe3\xe8\x67\xe6\xe0\xe3\x6a\xf9\x04\xd0\xaf\x69\xff\x20\x1f\x1e\x12\x83\xe2\x9e\x97\xd2\x27\xde\x16\xa3\x89\x06\x37\x4b\x19\xb1\x1b\x31\xe3\x54\xc2\x45\xbe\xe4\x8d\xd0\x73\x88\x28\xc9\xda\xc9\x54\xd7\x70\xea\x4c\x1b\x52\xad\xa0\x90\xc9\x49\x8e\x75\xd8\x37\x7e\x9c\xfe\x27\x00\x00\xff\xff\xec\x04\xfd\xa5\x23\x12\x00\x00")

func timeTimeVdlBytes() ([]byte, error) {
	return bindataRead(
		_timeTimeVdl,
		"time/time.vdl",
	)
}

func timeTimeVdl() (*asset, error) {
	bytes, err := timeTimeVdlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "time/time.vdl", size: 0, mode: os.FileMode(420), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _timeVdlConfig = []byte("\x1f\x8b\x08\x00\x00\x09\x6e\x88\x00\xff\xac\x54\x4d\x8b\xdb\x30\x10\x3d\xc7\xbf\x62\xf0\x69\x17\x82\x0c\xed\x2d\xd0\x43\xd9\xc0\x92\x7e\x78\x0b\x49\x5b\xe8\x6d\x6a\x4d\x1c\xb5\xb6\xc6\x95\x65\x2f\x25\xe4\xbf\x57\x23\x25\x21\xd9\x26\x85\xa5\x1b\x88\x62\xcf\xc7\x7b\xf3\x66\x26\x2a\x0a\x58\x6d\x08\x46\xdd\xa8\x8a\xed\xda\xd4\xb0\x36\x0d\xc1\x9a\x1d\xf8\x60\xef\x3d\x5a\x8d\x4e\x83\x37\x2d\x41\x87\xd5\x4f\xac\x09\x52\xe4\xe0\xa8\x07\xa3\x0d\xb7\xe8\x4d\x05\x36\x9c\x23\x65\x01\xd0\xff\xee\xc4\x63\x81\xb0\xda\x40\x4d\x96\x1c\x7a\xd2\xd0\xa0\xad\x87\x90\xaf\xb2\x3d\xd5\x1b\xe1\xf5\xcc\x8d\xba\x8b\x86\x6d\x36\xb9\xe7\x19\x84\x9f\xc9\x57\xe3\x68\xc5\x65\xc4\x5c\x09\x5e\x32\x4f\x02\xfc\x97\xf9\x87\x58\x8e\x9a\x0f\x01\xd7\xb0\x05\xd3\x83\xa3\x2e\x94\x43\x56\x78\xb0\x87\x7b\x3e\x0f\x51\x92\x9b\x1f\xde\xf2\x3d\xd8\xe4\xbd\xb1\x7a\x06\xe1\x53\x0e\xed\x77\x72\xd3\x68\x14\xba\x68\xcc\xcf\x20\xf2\xe4\x5d\xb4\x1d\x3b\x2f\xe5\x6c\x3f\xa1\xdf\xcc\x52\x54\x3e\x85\x12\x5b\x3a\xbc\xed\x76\x29\xf8\x1b\x39\x8e\x50\xdb\x8f\xac\x83\xf7\xb3\x35\xbf\x06\x4a\xce\x74\x9e\xea\x59\x49\x8f\xaf\x6b\x11\x77\xd2\x21\x4f\x7f\x6b\x58\x7a\x37\x54\xfe\xa2\x86\x98\xf0\x02\xf5\xdf\xa1\x65\x6b\x2a\x6c\xa6\xb0\xe8\x93\x33\x57\xe9\xe9\xe6\x36\xbf\xa6\x4b\x66\x39\x27\xd4\x8d\xb1\x97\xf4\x49\x5c\x5a\x9e\xfd\xcc\xf6\xa1\x49\xeb\x69\xf2\xb3\x34\x1f\x93\xae\xe9\x1e\x95\xe1\x62\x7c\xf5\xba\x08\x4b\xe8\x98\x7d\xf1\xbf\x8d\x88\x03\xba\xd4\x0d\x39\xe4\xfb\x0e\x47\xfc\xf7\x7a\x9f\xae\x68\xce\xae\x56\x3f\x58\xa3\xba\xb0\x87\x87\x1d\x78\x1a\x14\xfe\x68\xc7\x61\x1f\x68\x97\x8f\x66\xed\x9f\xc1\x5b\x2e\x05\x62\x11\xe6\xe3\x46\x6c\x9e\xf0\x95\x4b\xe1\x38\xc3\x97\x3b\xe4\x61\xfe\x70\xe3\x59\xeb\xc7\xdb\x19\xbc\xd5\xfa\x38\xd0\xc0\x01\xfd\xd0\x49\xef\xe3\xa5\x22\x2d\xe8\x2b\x67\x3a\xaf\xb2\x5d\xf6\x27\x00\x00\xff\xff\x46\x95\x86\x66\x7e\x04\x00\x00")

func timeVdlConfigBytes() ([]byte, error) {
	return bindataRead(
		_timeVdlConfig,
		"time/vdl.config",
	)
}

func timeVdlConfig() (*asset, error) {
	bytes, err := timeVdlConfigBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "time/vdl.config", size: 0, mode: os.FileMode(420), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _vdltoolConfigVdl = []byte("\x1f\x8b\x08\x00\x00\x09\x6e\x88\x00\xff\xe4\x5a\x49\x73\x1b\x37\xf6\x3f\x8b\x9f\x02\xa5\xfa\x57\xfd\x25\x17\x43\xce\x64\x96\x83\xa6\x72\x50\x24\x45\xa5\x8c\x23\x7b\x2c\x29\x99\x9a\x54\x0e\xe8\x6e\x90\x84\xd5\x6c\x30\x00\x9a\x12\xe3\xf2\x77\x9f\xb7\x60\x6b\x92\x92\xe2\x4c\x4e\xb1\x0f\x32\xd9\x0d\xbc\xf5\xf7\x36\x80\xd3\xa9\x38\x33\xab\x8d\xd5\xf3\x85\x17\x5f\xfe\xe9\xcf\x7f\x13\xb7\x0b\x25\xbe\x97\x9d\x6c\x74\xbf\x14\xa7\xbd\x5f\x18\xeb\x26\xe2\xb4\x6d\x05\x2d\x72\xc2\x2a\xa7\xec\x5a\x35\x93\x11\x6c\xbe\x73\x4a\x98\x99\xf0\x0b\xed\x84\x33\xbd\xad\x95\xa8\x4d\xa3\x04\x7c\x9d\x9b\xb5\xb2\x9d\x6a\x44\xb5\x11\x52\x7c\x7d\x73\xfe\x85\xf3\x9b\x56\xe1\xae\x56\xd7\xaa\x83\x9d\x7e\x21\xbd\xa8\x65\x27\x2a\x25\x66\xa6\xef\x1a\xa1\x3b\x78\xa8\xc4\xeb\xab\xb3\x8b\xeb\x9b\x0b\x31\xd3\xad\x9a\x8c\x70\xcb\x5b\x59\xdf\xcb\xb9\x12\xeb\xa6\xf5\xc6\xb4\xa2\x51\x33\xdd\x29\x27\xfc\x66\x05\x7f\x7b\xc7\x7c\x70\x2f\xac\x10\xb4\x44\x7b\xa7\xda\xd9\x18\x68\xd6\x6d\xdf\xe8\x6e\x8e\xaf\x91\xd6\xcc\xd8\x25\x30\x06\xb9\x61\xed\xa4\x36\xdd\x4c\xcf\x89\x95\x9b\x8c\x56\x43\x3e\xc4\xfb\x8c\x57\xb8\x95\xaa\xf5\x4c\x23\xd3\x05\xaa\x89\x0f\x7b\x2b\xbd\x36\x1d\x92\x1c\x30\x9f\x08\x30\x24\x18\x41\x93\x84\xba\x96\x6d\xbb\x41\x52\x56\xad\xd0\x7e\x9d\x57\xa4\xab\x59\xe1\x6e\xd9\x8a\xc3\x2c\xc9\x21\x8b\x82\xaf\x95\xac\x17\x44\x32\x98\x36\x08\x07\xc4\x2f\xe0\x0d\xd2\xdb\x52\x40\xe8\xe5\x0a\x8c\xab\x7d\xbb\xc1\x8f\xc6\x7a\xc7\xbe\x29\x76\x4e\xe6\x13\xb1\x31\xbd\x58\xca\x0d\x88\x33\x53\x20\xb8\x09\xa4\x48\xf2\xa0\x6d\xf0\xc4\x8e\x60\xe2\x41\x03\x26\x7a\x2f\xd4\xe3\x16\x2b\xb4\x70\x24\x32\x42\xbf\x24\xc3\x79\xdb\xd7\x5e\x7c\x18\x1d\x00\x9b\x4b\xd5\xbd\x96\xdd\xbc\x07\x69\x08\x4b\xde\xea\xda\xb3\x49\x9d\x22\xa7\x10\x80\xe6\xaa\x53\xc1\xb6\x6d\x5c\x0e\xd2\x5f\xcd\xd2\x4a\xed\x88\x9e\x5a\xae\xfc\x66\x2c\xc0\xc0\xc2\xf5\x2b\x94\x03\x4c\x9b\xb6\x08\x69\x15\xbe\x33\x0f\xf0\xd4\x1b\x04\x5a\xa0\x8c\x00\x3e\x18\x08\x03\x44\x7f\x2c\x1e\xfc\x34\x22\xfa\xf1\xeb\x17\xc1\xfd\xf5\xd0\xf3\x0e\xa9\x18\x11\xff\x5d\x1a\xd6\x79\x74\xf0\xad\x5c\xcb\xf0\x10\x3f\x96\x8f\x5d\x6d\xf5\xca\x8b\xfc\x31\xbe\xbc\x79\xd0\x33\xcf\x7b\xe8\x63\x78\xfe\x91\x50\x58\x88\x26\x54\xd7\x2f\x49\x09\x36\xdc\x7d\x67\x1e\xba\x67\xec\xc6\xde\xd8\x26\x80\x0e\xb9\x34\x2c\x52\x29\x58\x90\x23\xb2\x35\x3b\xf0\x9f\x1b\xb1\xdf\x18\x91\x91\xd9\xe7\xf8\x1f\xb4\x55\xb7\xe6\x1a\x16\xae\xd5\x2d\x45\xed\x30\xa0\x96\x72\xb5\x42\x08\xcd\xac\x59\x42\xc2\xf8\xfe\xfc\x35\x40\x0d\xbc\x47\x24\xc1\x75\x10\xce\x02\xc5\x05\x52\x1d\x11\xe1\x37\x29\xa2\x58\x82\x1c\x77\x16\x5c\x0f\xd8\xa4\xd4\x20\x21\xb5\x28\xe9\x36\x48\x27\x4a\xac\x88\x14\x24\x07\x63\xad\xaa\x01\xc6\xff\x80\xb5\x68\x1b\xd8\x5b\xf7\xf0\xac\x43\x68\x47\x84\x32\x7c\x42\x18\x39\xce\x5c\x88\xad\x1c\x05\x44\xee\x61\xa1\xbd\x6a\xb5\x63\x78\x4d\xa7\x4f\x68\x8e\x3b\x97\x4a\x76\x9e\x12\x87\x83\x6c\x28\xad\x36\x0e\xb6\x2b\xd4\x98\xfe\x82\x18\x90\x19\x75\xa3\x0d\x24\x2b\xb0\x74\xd0\x9d\x94\x26\xa5\x20\x42\x21\x8e\x2d\xf9\x7d\x2c\x2a\x08\x49\x0c\xeb\x4e\xa1\xbe\x60\x7a\xd0\x59\xda\x86\xec\x38\xb4\x11\xf1\x44\xd3\x12\xbd\xda\x2c\x57\xf0\xb8\xd2\xad\xf6\x9b\x98\x1f\xd0\x1f\xb8\xd1\xeb\x65\xca\x3a\x29\xe5\x9e\xc7\xa4\x87\x56\xbd\xc5\x15\x40\x90\xb5\x47\x7f\x0d\x08\xb2\x5c\x0f\x90\x35\x50\x59\x24\x9b\xc2\x8f\xf1\x0a\x46\xed\xa9\x12\xa8\x2c\x72\xd4\xb4\x60\x9e\x8d\x89\xf5\xe9\x5e\x6d\xb8\xea\x10\x6a\x28\xc9\xc2\xc7\x4e\x2e\x55\x7c\x4c\xc2\xa3\xa5\x8e\xe4\xbd\x64\x07\xc0\xb7\xe3\x31\x3a\x08\x72\xea\xb2\x77\x9e\xa8\x55\x51\xad\x54\x7a\x30\xdd\x46\x8d\xa5\x73\xa6\xd6\x24\x2c\x66\xbd\xf8\xbe\xcc\xb8\x43\xc1\xca\x10\x04\x23\xc3\x7e\x88\x53\x96\x4e\x3d\x22\x2a\xba\x9a\x44\x94\xc0\x42\x5b\x4e\x76\x1d\x94\x49\x47\x6e\xe9\xbb\x9a\x52\x4a\xf0\x0b\xbe\xa0\xa4\x5a\x29\xff\xa0\x14\x8b\x47\x26\x46\xc3\x17\x31\xe0\xc6\xf4\xe4\x41\x43\x0e\x94\xbd\x67\xbc\x60\xc5\x11\xf8\x97\xa8\xa1\xd4\xef\xd4\x1c\x45\xb0\x0c\xc4\xa4\x11\x58\x3f\x72\x26\x13\xba\xac\xd2\x29\xca\x1f\x0a\xe7\xc0\xbc\xd1\xa0\x68\xfa\x6f\x8c\x39\xa1\xd5\x82\xe8\x00\x18\x4c\x04\xfb\xd1\x23\xbe\x1d\x8b\x4e\xbc\xe2\x07\xc7\x42\x59\x1b\xc0\x92\x97\x7f\x03\x11\x9f\x36\xbc\x0a\x3b\xb6\x36\xec\xc6\x10\x78\xfe\x47\x8c\xcd\x6e\xfe\xd3\xa5\xc1\x47\x29\x61\x91\x64\x8d\xc2\x6c\x56\x05\xeb\x43\x8a\x26\x38\xe8\x8e\x8b\x3f\xc1\x77\xcb\xbb\x32\xa1\x86\x5a\x9b\x1b\x05\xce\x9e\x68\x33\x7d\x9c\x42\x99\x9c\xb6\xba\x9a\x82\x15\xa7\x90\x73\x7d\x23\xbd\x9c\x06\x07\x60\x30\xa9\x47\x09\x75\x37\x27\x5a\x96\x60\x90\xfd\xfe\xa9\xb1\xbb\x09\xe9\x1a\x3f\x83\x25\x6f\x89\xd5\x01\xbd\xba\x34\xf8\x1f\x03\x29\x18\xb6\x94\x3b\x84\x09\x60\x74\x18\x3f\xd8\xdd\x80\x4f\x65\xb3\x0d\x7c\xa0\x1b\x85\x00\x0b\x11\xdd\xff\x28\x6b\x8a\x9c\xfb\x0b\x7e\x5d\xcb\xb6\xa7\x72\x4a\x40\x43\x14\xd5\x0b\x55\xdf\x33\xea\x16\x72\xad\x8d\x05\x4a\xb4\xf3\xd2\xe0\x7f\x2c\x61\x70\x04\x6d\xc8\xde\x13\xd8\xf1\x59\xdd\xa8\x1c\x8f\x2e\xca\x15\xac\x95\xe1\x4e\x84\x12\xe4\xb9\xb4\xf7\x5d\x14\xaf\x19\xd3\x2e\x88\x4d\xd9\xb7\x3e\xaf\xa3\xcc\x19\x48\xa2\x27\x23\xf2\x22\x20\x13\xf2\x28\x4e\xe2\xd3\x71\x64\xff\x2a\x03\x68\x08\xc5\xb8\xb2\x80\x22\x51\x78\xb5\x43\x62\x0f\x85\x64\x8e\x68\xec\xc2\x24\x85\xfd\xaf\x42\x43\x26\x39\xc5\xa3\x6f\x63\x8f\x46\x7d\x18\x26\x87\xd4\xc5\x32\xcf\x27\x0c\xcd\xb6\xd3\xaa\x6d\xc8\x70\xa1\x51\xa4\x07\xe3\xdd\x74\xda\x1a\x48\x03\x29\xa7\xa1\x57\x38\x5f\xb0\xad\xb9\x6e\xe1\x06\x16\x26\x2d\xc4\x22\x94\xea\x41\x04\xce\xa6\x74\x26\x23\x13\xb2\xf4\x04\xab\x40\x74\x05\xa3\xff\x43\xf8\x26\x08\xf9\x27\xd4\xd2\x50\x3c\x8c\xd3\x0b\x5c\x46\x2f\x0e\x13\x8d\xc3\xfc\x16\xc1\x46\x6f\x3f\x7c\x07\x38\x3f\x11\x67\xb2\x33\x1d\xa6\xb5\xb1\xb8\x72\xfc\xf2\x70\xc2\x9f\x8e\x8e\x0f\x3f\xe6\x8d\xc1\xcc\x27\xe2\xc3\x87\xb7\xd2\x2f\x4e\x98\xfc\xe1\x18\x3c\x07\x42\x86\x6f\x1f\xd3\x86\x8f\xa3\x83\xe8\x98\x1f\x21\x91\xf0\xe7\x94\x4a\x28\x36\x87\xa9\x24\x46\x6f\x08\xcd\x14\xf3\xb4\x34\xf6\x55\xac\xeb\xe8\xe0\x6b\x9c\x21\x0e\xae\xfb\x65\xa5\x2c\x3d\x25\x30\x9c\x5a\x2b\xc1\xee\x37\x38\x07\x8d\x0e\xbe\x93\xab\xd1\xc1\x5b\xa3\x61\x28\x80\x35\x57\x33\x59\xe7\x54\xc6\xe2\x14\x12\x24\xd0\x94\xa9\x2c\xc9\x10\x96\x0f\x32\x0f\x1a\x21\xa6\x93\xe8\xdb\x95\xa4\xdc\x0f\x3d\x4c\xdf\xe9\x9f\x7b\xec\x93\x20\x6c\x3b\x9f\xbb\x31\x66\x02\x78\xcc\x05\x98\x08\x15\x78\x46\x7b\xee\xab\xbd\x91\x49\xa2\x48\xa0\xc6\xdd\x00\xa7\xf3\x9e\x92\xd9\xa5\xe1\x54\xd0\x51\x54\xb3\x2f\xb4\x1f\x8c\x4b\xe2\x3d\x14\x69\x22\x58\x49\xe8\x5d\x02\x03\x24\xc3\x4d\x05\xce\x2f\x15\x8f\x02\x08\x4f\xb3\x54\x30\xe9\x04\xd1\x1a\x3d\xa3\x80\x02\x8a\xb3\xbd\xea\x88\xc6\x28\xd7\xfd\x3f\xf6\x5f\x38\x1c\x6c\xc9\x03\xca\x92\x72\x41\xd9\xe8\x0c\x4a\x83\x03\x57\x14\x39\x34\xa7\x4b\x28\x9e\xb0\xfc\xdd\xc5\xbf\xee\xae\xde\x5d\x9c\x9f\x88\x0b\x8d\x0d\x9d\x40\x18\x8b\xaf\xbe\x12\x77\x64\x71\x01\x65\x83\xd1\x8b\x4a\x83\x12\x60\x9b\x1f\x14\xd7\xf0\xce\x40\x14\x2a\x8b\xee\x1d\xa5\x6e\x77\x1a\x9a\xab\x98\x3f\x1d\xf5\xb2\x98\xac\x0b\x29\x72\x05\xe2\x64\x9f\x70\x40\xcc\xf9\x29\x7e\xe4\x7c\xe4\xb6\x2a\x82\x8c\x29\x04\x4d\x69\x1a\x14\x31\x75\x06\x04\x16\xab\x7c\x6f\x91\xb3\x05\x05\xc0\xc4\x34\x50\x17\x0d\x39\x5b\x22\x75\x9b\x2e\x55\xa4\x2c\x61\x6e\x2e\x20\xdf\x47\x09\xbc\xc4\xf0\x0b\x45\xb8\x01\xed\x8f\x26\xd0\xaa\x31\x1e\xb8\x87\x8a\xc3\x5b\x10\x11\x45\x63\x21\x27\xa9\xf7\xe2\x17\xa4\x7e\x7a\x1b\x24\xe6\xa7\xd8\xf4\x21\x8d\x8a\xe7\xf3\xcb\x61\x23\x8a\x96\x27\x5a\x30\x86\xb4\x9c\xe6\x92\x78\x96\x46\x5b\x65\x2b\x50\x73\x89\x92\x40\xd7\x0f\x2a\xb2\x1b\x86\x05\x2e\xd4\xde\x1d\x1d\x23\xde\x48\xd7\x42\xd5\xe7\x34\x8d\xb6\x7f\x58\x18\xc7\x76\xfe\x34\x75\xa8\xfd\xe3\x52\x99\x48\x11\x63\xa2\xe5\x40\x29\x18\xe7\xa5\x9d\x03\x57\x6a\xce\xa5\x0f\x43\x47\x56\xef\x59\xed\x6e\xdf\x9c\xbf\x39\xf2\xa6\x69\x1e\x8e\x4f\xd8\x05\x91\x0d\x42\x17\xb7\x26\x84\xa3\xea\x78\x36\xa1\x96\x74\xf6\x01\x34\x06\xc6\xdd\x25\x77\xda\xe0\x14\x73\xa3\x3c\xad\x0a\xb8\xe4\xe6\x1d\x64\x6c\xf5\x7d\x74\x0f\x27\x83\x88\x4c\xc9\x7d\x45\xa1\x00\xa9\x05\xf1\xc5\x60\x2c\x81\x58\x86\x35\xc5\xc7\x30\xcf\xc3\xf4\xc8\x13\xfe\x42\xaf\x06\x2d\xf8\x30\xee\x07\x06\x1a\x85\x01\x8d\x0b\xec\x3e\xf4\x17\xe1\x49\x3c\x63\xc1\xc0\xb3\xb4\x8e\xe7\xf8\x72\x2c\x96\x7e\x0f\x4b\xc4\x12\x25\x89\x14\x69\x03\x56\xdc\xf0\x13\xbb\xe2\x14\x2a\x77\x53\x05\x3c\x3b\x3e\x1d\x5a\x12\x62\xf2\x6c\x26\x1c\xe6\xc4\x38\x9f\x85\x54\xf4\x64\x1e\x8a\xd3\x5f\xec\x22\xcb\x74\x04\x02\xec\xc9\x52\xc8\xcb\xaa\x9f\x7b\x1a\xef\xa3\x8e\xec\xce\xdc\xda\xec\x4d\x43\x21\x57\x8e\x0e\x82\xb1\x68\x79\x6a\x0d\x5e\x36\xdd\x2e\x9c\x83\x71\x78\xe6\x4a\x84\xb6\x26\xe6\xa2\xb9\xce\xc4\x18\x79\x9a\x5d\x81\xaf\x4d\x07\x89\x63\xe8\x81\xdd\xe3\x89\x34\x5f\x0f\xa5\x8a\x12\xc5\x4e\xe8\xd7\x8a\xf5\x4b\x0a\x01\x0e\xb2\x81\x1c\x18\xe3\x35\x73\x82\xd0\x68\x9d\x09\xc1\x01\x63\x20\xcd\x2d\xb9\x4c\x62\x8f\x18\x4b\xdf\x4b\xe0\x98\x49\xd8\x4e\xe0\xa0\xc3\x8b\x97\xbd\x9e\xcd\xf1\x9c\xe7\x7f\x8d\xd7\x93\xa7\x43\xc0\x50\x25\xfd\xed\x4e\x27\x4b\x85\xc4\xfa\xb2\xbf\x9f\x76\x1e\x79\x2d\x1e\x8b\x10\xbd\x23\x4e\x55\x0b\xe9\xa8\x7b\x89\xc3\xa7\xff\xfb\x5f\x8f\x4b\xde\x4f\x38\xf4\x93\x9d\x50\x19\x6c\xcd\xf6\x4d\x71\x5b\xf6\xa7\xc8\x41\xab\x85\xec\x97\x8f\x26\x0b\x33\xbe\xc7\xa3\xcb\x67\x4f\xf8\xca\x6d\xbf\xf7\x19\x1f\x1f\x47\x7e\xae\xa7\x7c\x49\xfb\x3f\xee\x39\x9f\xb1\xf3\xc9\x7b\xd3\xc8\x09\x72\x25\x7a\x7f\x90\x43\xbe\x90\x7c\xf6\x1d\xe9\x71\x3f\x56\x21\x90\xf9\x10\x4e\x35\x01\x98\x1c\x6d\x85\x40\x93\x17\x0e\xbb\x42\xc7\x94\x31\x07\x0b\xde\x29\x3e\x6c\xf9\x84\x68\x23\x43\x72\xc8\x11\x29\xba\xaf\x28\x43\x0e\x17\x6c\xa1\xbb\xe0\xf4\x22\xb6\x33\x3b\xdc\x10\x8f\x34\x67\x2d\xdd\xf6\x80\xda\x38\xb0\x01\x5a\x36\xd9\xe8\xa5\x04\xc4\x64\x2c\x14\x80\x74\x1c\x44\xbf\x82\xc6\x71\xae\x6c\x0e\x8e\xda\xf4\xd0\xf0\x57\xbc\x18\x02\x20\x2c\x20\x81\xea\x16\xc4\x01\x9c\x67\xf9\x17\x0a\x6f\x48\x96\x15\xc1\x80\x78\xee\x58\x79\xcc\xb9\x76\xa6\x24\x34\x93\xe1\xda\x08\xef\x16\xd1\x4c\xd2\x7b\xbc\x72\x1b\x1c\x5a\xe1\xf3\x88\x41\x2c\x7a\xdf\xe4\xa3\xc0\x31\xc6\x02\xdf\xb0\x91\xa1\x4f\xeb\x1a\xfa\xd1\xd7\x58\x78\xe3\x8e\xe0\x7f\xa0\x81\x82\x14\x0b\x42\xeb\xd8\x11\xec\xb7\xde\xc1\x6a\x4c\x05\xdb\xce\xe2\xd3\xda\xbc\x8c\x03\x20\x06\x72\x16\xd8\x74\xc9\x35\x78\x86\xf9\x5b\x02\x2e\x21\xa8\x0c\x33\xf0\x02\xcf\xdd\xff\x6b\x98\x6d\xc3\x6c\x17\xf7\xb9\x6e\x95\x77\x67\x5b\xd5\x2b\xdc\xb0\xbd\x58\xc3\x86\x24\x62\x25\x63\x16\xc5\x0d\x5c\x41\xdd\xd1\x15\xdd\x7e\xc2\xe1\x16\xb8\xb8\x6e\x45\x3a\xd7\xc6\x87\x5e\x07\xe6\x8b\x95\xf6\x1c\x1d\x25\x75\xbe\x05\xa6\x46\x1a\xb0\x3e\x07\xa7\x76\xc9\x6c\xa1\x91\x70\x8b\x00\x76\xba\xa9\x55\x76\x43\x85\x0f\xed\xbc\xb2\x12\x9c\x5b\x53\xda\xc3\x13\x0b\x3a\x40\x50\x5c\xc8\xb0\x45\xde\x71\x22\x6b\x00\x0d\x45\xcf\x97\xf1\x21\x6f\x65\xa9\x79\xf2\x51\x04\x6f\xac\x86\xd6\xc0\x04\x80\x35\xb6\x6b\x20\xad\x61\xbd\x89\x47\x50\x15\xde\xd9\x43\xa0\xc1\xda\x30\x20\x21\x41\xd8\xf0\x1e\x4a\xad\x9b\xce\x2c\xb0\x7d\x30\xf6\xde\x4d\x99\x9d\x8b\x87\x23\x3f\xe0\x10\xc3\x4c\xab\x0d\xa3\x86\x3a\x17\xbe\x61\xc6\xb1\x15\x14\x38\x24\x49\x79\xe7\x61\xf8\xad\x80\x81\xda\xa6\xc1\x54\xdf\xde\xdd\xdc\xc6\xeb\xfc\x52\xb9\x9b\x42\x39\x11\xf2\xb1\x35\x86\xee\x95\xa9\x88\x22\x84\x63\x7d\x9f\x0c\x22\x16\x49\x69\x5a\x05\x6d\xdb\x3a\xe7\x30\xbe\x8d\x47\xdf\x10\x81\x7f\x53\xfa\x0f\x3a\x4e\x61\x9a\x9f\x2b\x1f\x25\xbe\xab\x94\x05\x92\x67\x12\x72\x2a\x9e\x24\x78\x31\xbd\x73\x60\xb3\xa9\x94\xd6\x74\xd3\xbe\xa2\x49\x0a\x3a\x6d\x87\x57\x09\x63\x8e\xf2\xda\x42\xbe\x21\x45\x9e\x5b\x3c\x85\xdc\x35\x29\x9f\x15\xc6\xa1\x8c\x41\x32\x6b\xcf\xa7\x66\xc1\x4e\x48\xf4\xb0\x10\xea\x70\x82\x96\x27\xae\x54\x8f\x3c\xb2\x86\xdd\x9b\x42\xd1\xb0\x35\x06\x31\xc2\x56\x63\xe7\x64\x00\x73\xc0\x87\xf4\x82\x0d\xae\xaf\xe2\x73\x3a\x3a\x42\xf4\x58\x1f\xdd\x50\xf0\x1c\xb8\x64\x2c\x60\xfe\xd4\xd0\xe9\x60\xcf\xd6\x72\xbf\x3a\x0a\xc7\xcf\xd8\x4f\x80\x04\xf6\xa9\xbb\x2c\x02\x8c\xe2\x5f\xa1\x08\x3a\xea\x43\x77\x19\x6c\xa3\x5c\x04\x24\x79\xa8\x0c\xbe\x2b\x76\x68\x63\x70\x36\x8a\x90\x23\x03\x74\xa6\x32\xcd\x26\xd5\x65\x59\xb5\x5c\x0e\x0b\x42\x29\xb7\xa3\x29\x48\x8d\x31\xab\xdf\xf0\x0f\x28\x48\x12\x05\xdf\xfa\x15\xe7\x34\xf5\xe8\xad\x84\x59\x0a\x44\x83\x02\xe4\xdc\x74\x75\x3f\x0f\xd7\x20\x47\x67\x66\x59\xb8\xef\x46\xd9\x35\x04\xad\x7b\x6b\xcd\x4c\x7b\x82\x5e\xbe\xc7\x19\xbe\x24\xe8\xfd\x1f\xc8\xf2\xee\xcd\x9b\xdb\x5d\x18\x84\xb5\xd3\x15\x2d\x3e\x8e\x11\x16\x14\xc7\x24\x41\x00\x43\x73\x2d\xd1\xf8\x00\xf5\x81\x4b\x86\x69\x27\xf2\x61\x1b\x11\x96\x56\x2d\x1e\x48\xe3\x7c\x5f\x20\x8e\xb1\x12\x02\xac\x35\x73\x9a\x4e\x73\x3a\x08\x91\x95\x4b\x61\x34\x3d\xf2\xa2\x4b\xb5\xf5\x97\x7f\x21\xc7\xd3\x59\x17\xb4\xd7\x7c\x3a\xeb\xd9\xc7\xf1\xf7\x47\x67\x06\xc4\x4f\x59\x84\x7b\x4c\x85\xd9\x8f\xdd\x4f\x09\x29\x91\x4b\xa6\xe0\x5f\x18\xad\xd9\x6d\x25\xb9\x68\xd7\x4c\x32\x94\x82\x41\xaa\xff\xbd\xe7\x19\xfe\x31\xc5\x67\x3b\xd0\x64\xf5\xff\x98\x13\xcd\xf5\xcd\x39\xce\xe3\x20\xd8\xf5\x0d\x92\xc4\x2e\xd4\xae\xf9\x84\xe2\xb3\x9b\x65\x38\xb1\x7c\xfa\x30\xf3\x71\xf4\xdf\x00\x00\x00\xff\xff\xf9\x86\x52\x63\x89\x28\x00\x00")

func vdltoolConfigVdlBytes() ([]byte, error) {
	return bindataRead(
		_vdltoolConfigVdl,
		"vdltool/config.vdl",
	)
}

func vdltoolConfigVdl() (*asset, error) {
	bytes, err := vdltoolConfigVdlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "vdltool/config.vdl", size: 0, mode: os.FileMode(420), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"math/math.vdl":           mathMathVdl,
	"math/vdl.config":         mathVdlConfig,
	"signature/signature.vdl": signatureSignatureVdl,
	"time/time.vdl":           timeTimeVdl,
	"time/vdl.config":         timeVdlConfig,
	"vdltool/config.vdl":      vdltoolConfigVdl,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"math": &bintree{nil, map[string]*bintree{
		"math.vdl":   &bintree{mathMathVdl, map[string]*bintree{}},
		"vdl.config": &bintree{mathVdlConfig, map[string]*bintree{}},
	}},
	"signature": &bintree{nil, map[string]*bintree{
		"signature.vdl": &bintree{signatureSignatureVdl, map[string]*bintree{}},
	}},
	"time": &bintree{nil, map[string]*bintree{
		"time.vdl":   &bintree{timeTimeVdl, map[string]*bintree{}},
		"vdl.config": &bintree{timeVdlConfig, map[string]*bintree{}},
	}},
	"vdltool": &bintree{nil, map[string]*bintree{
		"config.vdl": &bintree{vdltoolConfigVdl, map[string]*bintree{}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}

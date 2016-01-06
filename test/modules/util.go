// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modules

import (
	"fmt"
	"hash/adler32"
	"io"
	"io/ioutil"
	"os"

	"v.io/x/ref/internal/logger"
)

func newLogfile(prefix, name string) (*os.File, error) {
	nameHash := adler32.Checksum([]byte(name))
	f, err := ioutil.TempFile("", fmt.Sprintf("__modules__%s-%x-", prefix, nameHash))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func outputFromFile(f *os.File, out io.Writer) {
	f.Close()
	fName := f.Name()
	defer os.Remove(fName)
	if out == nil {
		return
	}
	var err error
	if f, err = os.Open(fName); err != nil {
		logger.Global().VI(1).Infof("failed to open %q: %s\n", fName, err)
		return
	}
	io.Copy(out, f)
	f.Close()
}

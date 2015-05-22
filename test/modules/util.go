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

	"v.io/v23/security"
	"v.io/x/lib/vlog"
	vsecurity "v.io/x/ref/lib/security"
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
		vlog.VI(1).Infof("failed to open %q: %s\n", fName, err)
		return
	}
	io.Copy(out, f)
	f.Close()
}

func principalFromDir(dir string) (security.Principal, error) {
	p, err := vsecurity.LoadPersistentPrincipal(dir, nil)
	if err == nil {
		return p, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	p, err = vsecurity.CreatePersistentPrincipal(dir, nil)
	if err != nil {
		return nil, err
	}
	if err := vsecurity.InitDefaultBlessings(p, shellBlessingExtension); err != nil {
		return nil, err
	}
	return p, nil
}

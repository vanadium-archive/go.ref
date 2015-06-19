// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A test for localblobstore
package localblobstore_test

import "io/ioutil"
import "os"
import "testing"

import "v.io/syncbase/x/ref/services/syncbase/localblobstore"
import "v.io/syncbase/x/ref/services/syncbase/localblobstore/fs_cablobstore"
import "v.io/syncbase/x/ref/services/syncbase/localblobstore/localblobstore_testlib"
import "v.io/x/ref/test"
import _ "v.io/x/ref/runtime/factories/generic"

// This test case tests adding files, retrieving them and deleting them.  One
// can't retrieve or delete something that hasn't been created, so it's all one
// test case.
func TestAddRetrieveAndDelete(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	// Make a temporary directory.
	var err error
	var testDirName string
	testDirName, err = ioutil.TempDir("", "localblobstore_test")
	if err != nil {
		t.Fatalf("localblobstore_test: can't make tmp directory: %v\n", err)
	}
	defer os.RemoveAll(testDirName)

	// Create an fs_cablobstore.
	var bs localblobstore.BlobStore
	bs, err = fs_cablobstore.Create(ctx, testDirName)
	if err != nil {
		t.Fatalf("fs_cablobstore.Create failed: %v", err)
	}

	// Test it.
	localblobstore_testlib.AddRetrieveAndDelete(t, ctx, bs, testDirName)
}

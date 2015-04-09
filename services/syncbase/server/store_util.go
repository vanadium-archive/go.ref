// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"strconv"

	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/v23/vom"

	"v.io/syncbase/x/ref/services/syncbase/store"
)

func getObject(st store.Store, k string, v interface{}) error {
	bytes, err := st.Get(k)
	if err != nil {
		return err
	}
	return vom.Decode(bytes, v)
}

func putObject(st store.Store, k string, v interface{}) error {
	bytes, err := vom.Encode(v)
	if err != nil {
		return err
	}
	return st.Put(k, bytes)
}

func formatVersion(version uint64) string {
	return strconv.FormatUint(version, 10)
}

func checkVersion(call rpc.ServerCall, presented string, actual uint64) error {
	if presented != "" && presented != formatVersion(actual) {
		return verror.NewErrBadVersion(call.Context())
	}
	return nil
}

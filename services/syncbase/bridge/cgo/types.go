// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"strings"
	"unsafe"

	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
)

// All "x.toFoo" methods free the memory associated with x.

/*
#include <stdlib.h>
#include <string.h>
#include "lib.h"
*/
import "C"

////////////////////////////////////////
// C.v23_syncbase_String

func (x *C.v23_syncbase_String) init(s string) {
	x.n = C.int(len(s))
	x.p = C.CString(s)
}

func (x *C.v23_syncbase_String) toString() string {
	defer C.free(unsafe.Pointer(x.p))
	return C.GoStringN(x.p, x.n)
}

////////////////////////////////////////
// C.v23_syncbase_Bytes

func init() {
	if C.sizeof_uint8_t != 1 {
		panic(C.sizeof_uint8_t)
	}
}

func (x *C.v23_syncbase_Bytes) init(b []byte) {
	x.n = C.int(len(b))
	x.p = (*C.uint8_t)(C.malloc(C.size_t(len(b))))
	C.memcpy(unsafe.Pointer(x.p), unsafe.Pointer(&b[0]), C.size_t(len(b)))
}

func (x *C.v23_syncbase_Bytes) toBytes() []byte {
	defer C.free(unsafe.Pointer(x.p))
	return C.GoBytes(unsafe.Pointer(x.p), x.n)
}

////////////////////////////////////////
// C.v23_syncbase_Strings

func (x *C.v23_syncbase_Strings) at(i int) *C.v23_syncbase_String {
	return (*C.v23_syncbase_String)(unsafe.Pointer(uintptr(unsafe.Pointer(x.p)) + uintptr(C.size_t(i)*C.sizeof_v23_syncbase_String)))
}

func (x *C.v23_syncbase_Strings) init(strs []string) {
	x.n = C.int(len(strs))
	x.p = (*C.v23_syncbase_String)(C.malloc(C.size_t(len(strs)) * C.sizeof_v23_syncbase_String))
	for i, v := range strs {
		x.at(i).init(v)
	}
}

func (x *C.v23_syncbase_Strings) toStrings() []string {
	defer C.free(unsafe.Pointer(x.p))
	res := make([]string, x.n)
	for i := 0; i < int(x.n); i++ {
		res[i] = x.at(i).toString()
	}
	return res
}

////////////////////////////////////////
// C.v23_syncbase_VError

func (x *C.v23_syncbase_VError) init(err error) {
	if err == nil {
		return
	}
	x.id.init(string(verror.ErrorID(err)))
	x.actionCode = C.uint(verror.Action(err))
	x.msg.init(err.Error())
	x.stack.init(verror.Stack(err).String())
}

////////////////////////////////////////
// C.v23_syncbase_Permissions

func (x *C.v23_syncbase_Permissions) init(perms access.Permissions) {
	b := new(bytes.Buffer)
	if err := access.WritePermissions(b, perms); err != nil {
		panic(err)
	}
	x.json.init(b.String())
}

func (x *C.v23_syncbase_Permissions) toPermissions() access.Permissions {
	perms, err := access.ReadPermissions(strings.NewReader(x.json.toString()))
	if err != nil {
		panic(err)
	}
	return perms
}

////////////////////////////////////////
// C.v23_syncbase_Id

func (x *C.v23_syncbase_Id) init(id wire.Id) {
	x.blessing.init(id.Blessing)
	x.name.init(id.Name)
}

func (x *C.v23_syncbase_Id) toId() wire.Id {
	return wire.Id{
		Blessing: x.blessing.toString(),
		Name:     x.name.toString(),
	}
}

////////////////////////////////////////
// C.v23_syncbase_Ids

func (x *C.v23_syncbase_Ids) at(i int) *C.v23_syncbase_Id {
	return (*C.v23_syncbase_Id)(unsafe.Pointer(uintptr(unsafe.Pointer(x.p)) + uintptr(C.size_t(i)*C.sizeof_v23_syncbase_Id)))
}

func (x *C.v23_syncbase_Ids) init(ids []wire.Id) {
	x.n = C.int(len(ids))
	x.p = (*C.v23_syncbase_Id)(C.malloc(C.size_t(len(ids)) * C.sizeof_v23_syncbase_Id))
	for i, v := range ids {
		x.at(i).init(v)
	}
}

////////////////////////////////////////
// C.v23_syncbase_BatchOptions

func (x *C.v23_syncbase_BatchOptions) init(opts wire.BatchOptions) {
	x.hint.init(opts.Hint)
	x.readOnly = C.bool(opts.ReadOnly)
}

func (x *C.v23_syncbase_BatchOptions) toBatchOptions() wire.BatchOptions {
	return wire.BatchOptions{
		Hint:     x.hint.toString(),
		ReadOnly: bool(x.readOnly),
	}
}

////////////////////////////////////////
// C.v23_syncbase_KeyValue

func (x *C.v23_syncbase_KeyValue) init(key string, value []byte) {
	x.key.init(key)
	x.value.init(value)
}

// FIXME(sadovsky): Implement stubbed-out methods below.

////////////////////////////////////////
// C.v23_syncbase_SyncgroupSpec

func (x *C.v23_syncbase_SyncgroupSpec) init(spec wire.SyncgroupSpec) {

}

func (x *C.v23_syncbase_SyncgroupSpec) toSyncgroupSpec() wire.SyncgroupSpec {
	return wire.SyncgroupSpec{}
}

////////////////////////////////////////
// C.v23_syncbase_SyncgroupMemberInfo

func (x *C.v23_syncbase_SyncgroupMemberInfo) init(member wire.SyncgroupMemberInfo) {

}

func (x *C.v23_syncbase_SyncgroupMemberInfo) toSyncgroupMemberInfo() wire.SyncgroupMemberInfo {
	return wire.SyncgroupMemberInfo{}
}

////////////////////////////////////////
// C.v23_syncbase_SyncgroupMemberInfoMap

func (x *C.v23_syncbase_SyncgroupMemberInfoMap) init(members map[string]wire.SyncgroupMemberInfo) {

}

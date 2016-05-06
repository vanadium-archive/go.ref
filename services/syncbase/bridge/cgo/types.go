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
// C.XString

func (x *C.XString) init(s string) {
	x.n = C.int(len(s))
	x.p = C.CString(s)
}

func (x *C.XString) toString() string {
	defer C.free(unsafe.Pointer(x.p))
	return C.GoStringN(x.p, x.n)
}

////////////////////////////////////////
// C.XBytes

func init() {
	if C.sizeof_uint8_t != 1 {
		panic(C.sizeof_uint8_t)
	}
}

func (x *C.XBytes) init(b []byte) {
	x.n = C.int(len(b))
	x.p = (*C.uint8_t)(C.malloc(C.size_t(len(b))))
	C.memcpy(x.p, unsafe.Pointer(&b[0]), C.size_t(len(b)))
}

func (x *C.XBytes) toBytes() []byte {
	defer C.free(x.p)
	return C.GoBytes(x.p, x.n)
}

////////////////////////////////////////
// C.XStrings

func (x *C.XStrings) at(i int) *C.XString {
	return (*C.XString)(unsafe.Pointer(uintptr(unsafe.Pointer(x.p)) + uintptr(C.size_t(i)*C.sizeof_XString)))
}

func (x *C.XStrings) init(strs []string) {
	x.n = C.int(len(strs))
	x.p = (*C.XString)(C.malloc(C.size_t(len(strs)) * C.sizeof_XString))
	for i, v := range strs {
		x.at(i).init(v)
	}
}

func (x *C.XStrings) toStrings() []string {
	defer C.free(x.p)
	res := make([]string, x.n)
	for i := 0; i < int(x.n); i++ {
		res[i] = x.at(i).toString()
	}
	return res
}

////////////////////////////////////////
// C.XVError

func (x *C.XVError) init(err error) {
	if err == nil {
		return
	}
	x.id.init(string(verror.ErrorID(err)))
	x.actionCode = C.uint(verror.Action(err))
	x.msg.init(err.Error())
	x.stack.init(verror.Stack(err).String())
}

////////////////////////////////////////
// C.XPermissions

func (x *C.XPermissions) init(perms access.Permissions) {
	b := new(bytes.Buffer)
	if err := access.WritePermissions(b, perms); err != nil {
		panic(err)
	}
	x.json.init(b.String())
}

func (x *C.XPermissions) toPermissions() access.Permissions {
	perms, err := access.ReadPermissions(strings.NewReader(x.json.toString()))
	if err != nil {
		panic(err)
	}
	return perms
}

////////////////////////////////////////
// C.XId

func (x *C.XId) init(id wire.Id) {
	x.blessing.init(id.Blessing)
	x.name.init(id.Name)
}

func (x *C.XId) toId() wire.Id {
	return wire.Id{
		Blessing: x.blessing.toString(),
		Name:     x.name.toString(),
	}
}

////////////////////////////////////////
// C.XIds

func (x *C.XIds) at(i int) *C.XId {
	return (*C.XId)(unsafe.Pointer(uintptr(unsafe.Pointer(x.p)) + uintptr(C.size_t(i)*C.sizeof_XId)))
}

func (x *C.XIds) init(ids []wire.Id) {
	x.n = C.int(len(ids))
	x.p = (*C.XId)(C.malloc(C.size_t(len(ids)) * C.sizeof_XId))
	for i, v := range ids {
		x.at(i).init(v)
	}
}

////////////////////////////////////////
// C.XBatchOptions

func (x *C.XBatchOptions) init(opts wire.BatchOptions) {
	x.hint.init(opts.Hint)
	x.readOnly = C.bool(opts.ReadOnly)
}

func (x *C.XBatchOptions) toBatchOptions() wire.BatchOptions {
	return wire.BatchOptions{
		Hint:     x.hint.toString(),
		ReadOnly: bool(x.readOnly),
	}
}

////////////////////////////////////////
// C.XKeyValue

func (x *C.XKeyValue) init(key string, value []byte) {
	x.key.init(key)
	x.value.init(value)
}

// FIXME(sadovsky): Implement stubbed-out methods below.

////////////////////////////////////////
// C.XSyncgroupSpec

func (x *C.XSyncgroupSpec) init(spec wire.SyncgroupSpec) {

}

func (x *C.XSyncgroupSpec) toSyncgroupSpec() wire.SyncgroupSpec {
	return wire.SyncgroupSpec{}
}

////////////////////////////////////////
// C.XSyncgroupMemberInfo

func (x *C.XSyncgroupMemberInfo) init(member wire.SyncgroupMemberInfo) {

}

func (x *C.XSyncgroupMemberInfo) toSyncgroupMemberInfo() wire.SyncgroupMemberInfo {
	return wire.SyncgroupMemberInfo{}
}

////////////////////////////////////////
// C.XSyncgroupMemberInfoMap

func (x *C.XSyncgroupMemberInfoMap) init(members map[string]wire.SyncgroupMemberInfo) {

}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"unsafe"

	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
)

/*
#include <stdlib.h>
#include "lib.h"
*/
import "C"

// TODO(sadovsky): Switch from C.CString to C.CBytes once C.CBytes is available.
// https://github.com/golang/go/issues/14838

////////////////////////////////////////
// C.XString

func (x *C.XString) free() {
	C.free(unsafe.Pointer(x.p))
}

func (x *C.XString) init(s string) {
	x.p = C.CString(s)
	x.n = C.int(len(s))
}

// Frees the memory associated with this object.
func (x *C.XString) toString() string {
	defer x.free()
	return C.GoStringN(x.p, x.n)
}

////////////////////////////////////////
// C.XBytes

func (x *C.XBytes) free() {
	C.free(x.p)
}

func (x *C.XBytes) init(b []byte) {
	x.p = unsafe.Pointer(C.CString(string(b)))
	x.n = C.int(len(b))
}

// Frees the memory associated with this object.
func (x *C.XBytes) toBytes() []byte {
	defer x.free()
	return C.GoBytes(x.p, x.n)
}

////////////////////////////////////////
// C.XVError

func (x *C.XVError) init(e error) {
	if e == nil {
		return
	}
	x.id.init(string(verror.ErrorID(e)))
	x.actionCode = C.uint(verror.Action(e))
	x.msg.init(e.Error())
	x.stack.init(verror.Stack(e).String())
}

// FIXME(sadovsky): Implement stubbed-out methods below.

////////////////////////////////////////
// C.XPermissions

func (x *C.XPermissions) free() {

}

func (x *C.XPermissions) init(perms access.Permissions) {

}

func (x *C.XPermissions) toPermissions() access.Permissions {
	return access.Permissions{}
}

////////////////////////////////////////
// C.XId

func (x *C.XId) free() {

}

func (x *C.XId) init(id wire.Id) {

}

////////////////////////////////////////
// C.XIds

func (x *C.XIds) free() {

}

func (x *C.XIds) init(ids []wire.Id) {

}

////////////////////////////////////////
// C.XBatchOptions

func (x *C.XBatchOptions) free() {

}

func (x *C.XBatchOptions) init(opts wire.BatchOptions) {

}

func (x *C.XBatchOptions) toBatchOptions() wire.BatchOptions {
	return wire.BatchOptions{}
}

////////////////////////////////////////
// C.XKeyValue

func (x *C.XKeyValue) free() {

}

func (x *C.XKeyValue) init(key string, value []byte) {

}

////////////////////////////////////////
// C.XSyncgroupSpec

func (x *C.XSyncgroupSpec) free() {

}

func (x *C.XSyncgroupSpec) init(spec wire.SyncgroupSpec) {

}

func (x *C.XSyncgroupSpec) toSyncgroupSpec() wire.SyncgroupSpec {
	return wire.SyncgroupSpec{}
}

////////////////////////////////////////
// C.XSyncgroupMemberInfo

func (x *C.XSyncgroupMemberInfo) free() {

}

func (x *C.XSyncgroupMemberInfo) init(member wire.SyncgroupMemberInfo) {

}

func (x *C.XSyncgroupMemberInfo) toSyncgroupMemberInfo() wire.SyncgroupMemberInfo {
	return wire.SyncgroupMemberInfo{}
}

////////////////////////////////////////
// C.XSyncgroupMemberInfoMap

func (x *C.XSyncgroupMemberInfoMap) free() {

}

func (x *C.XSyncgroupMemberInfoMap) init(members map[string]wire.SyncgroupMemberInfo) {

}

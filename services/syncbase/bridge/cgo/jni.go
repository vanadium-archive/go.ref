// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build java android
// +build cgo

package main

import (
	"unsafe"
)

/*
#include <stdlib.h>
#include <string.h>
#include "jni_wrapper.h"
#include "lib.h"

static jvalue* allocJValueArray(int elements) {
  return malloc(sizeof(jvalue) * elements);
}

static void setJValueArrayElement(jvalue* arr, int index, jvalue val) {
  arr[index] = val;
}
*/
import "C"

var (
	jVM                         *C.JavaVM
	arrayListClass              jArrayListClass
	hashMapClass                jHashMap
	idClass                     jIdClass
	syncgroupMemberInfoClass    jSyncgroupMemberInfo
	syncgroupSpecClass          jSyncgroupSpec
	verrorClass                 jVErrorClass
	versionedSyncgroupSpecClass jVersionedSyncgroupSpec
)

// JNI_OnLoad is called when System.loadLibrary is called. We need to cache the
// *JavaVM because that's the only way to get hold of a JNIEnv that is needed
// for any JNI operation.
//
// Reference: https://developer.android.com/training/articles/perf-jni.html#native_libraries
//
//export JNI_OnLoad
func JNI_OnLoad(vm *C.JavaVM, reserved unsafe.Pointer) C.jint {
	var env *C.JNIEnv
	if C.GetEnv(vm, unsafe.Pointer(&env), C.JNI_VERSION_1_6) != C.JNI_OK {
		return C.JNI_ERR
	}
	jVM = vm

	v23_syncbase_Init(C.v23_syncbase_Bool(1))
	arrayListClass = newJArrayListClass(env)
	hashMapClass = newJHashMap(env)
	idClass = newJIdClass(env)
	syncgroupMemberInfoClass = newJSyncgroupMemberInfo(env)
	syncgroupSpecClass = newJSyncgroupSpec(env)
	verrorClass = newJVErrorClass(env)
	versionedSyncgroupSpecClass = newJVersionedSyncgroupSpec(env)

	return C.JNI_VERSION_1_6
}

func Java_io_v_syncbase_internal_Service_GetPermissions(env *C.JNIEnv, cls C.jclass) C.jobject {
	return nil
}
func Java_io_v_syncbase_internal_Service_SetPermissions(env *C.JNIEnv, cls C.jclass, obj C.jobject) {}

//export Java_io_v_syncbase_internal_Service_ListDatabases
func Java_io_v_syncbase_internal_Service_ListDatabases(env *C.JNIEnv, cls C.jclass) C.jobject {
	var cIds C.v23_syncbase_Ids
	var cErr C.v23_syncbase_VError
	v23_syncbase_ServiceListDatabases(&cIds, &cErr)
	obj := C.NewObjectA(env, arrayListClass.class, arrayListClass.init, nil)
	// TODO(razvanm): populate the obj based on the data from cIds.
	return obj
}

func Java_io_v_syncbase_internal_Database_GetPermissions(env *C.JNIEnv, cls C.jclass, name C.jstring) C.jobject {
	return nil
}
func Java_io_v_syncbase_internal_Database_SetPermissions(env *C.JNIEnv, cls C.jclass, name C.jstring, perms C.jobject) {
}

// maybeThrowException takes ownership of cErr and throws a Java exception if
// cErr represents a non-nil error. Returns a boolean indicating whether an
// exception was thrown.
func maybeThrowException(env *C.JNIEnv, cErr *C.v23_syncbase_VError) bool {
	if obj := cErr.extractToJava(env); obj != nil {
		C.Throw(env, obj)
		return true
	}
	return false
}

//export Java_io_v_syncbase_internal_Database_Create
func Java_io_v_syncbase_internal_Database_Create(env *C.JNIEnv, cls C.jclass, name C.jstring, perms C.jobject) {
	cName := newVStringFromJava(env, name)
	var cErr C.v23_syncbase_VError
	// TODO(razvanm): construct a proper C.v23_syncbase_Permissions based on
	// the perms object.
	v23_syncbase_DbCreate(cName, C.v23_syncbase_Permissions{}, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_Destroy
func Java_io_v_syncbase_internal_Database_Destroy(env *C.JNIEnv, cls C.jclass, name C.jstring) {
	cName := newVStringFromJava(env, name)
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbDestroy(cName, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_Exists
func Java_io_v_syncbase_internal_Database_Exists(env *C.JNIEnv, cls C.jclass, name C.jstring) C.jboolean {
	cName := newVStringFromJava(env, name)
	var r C.v23_syncbase_Bool
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbExists(cName, &r, &cErr)
	maybeThrowException(env, &cErr)
	return C.jboolean(r)
}

//export Java_io_v_syncbase_internal_Database_BeginBatch
func Java_io_v_syncbase_internal_Database_BeginBatch(env *C.JNIEnv, cls C.jclass, name C.jstring, opts C.jobject) C.jstring {
	cName := newVStringFromJava(env, name)
	var cHandle C.v23_syncbase_String
	var cErr C.v23_syncbase_VError
	// TODO(razvanm): construct a C.v23_syncbase_BatchOptions from opts.
	v23_syncbase_DbBeginBatch(cName, C.v23_syncbase_BatchOptions{}, &cHandle, &cErr)
	if maybeThrowException(env, &cErr) {
		return nil
	}
	return cHandle.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Database_ListCollections
func Java_io_v_syncbase_internal_Database_ListCollections(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) C.jobject {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var cIds C.v23_syncbase_Ids
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbListCollections(cName, cHandle, &cIds, &cErr)
	if maybeThrowException(env, &cErr) {
		return nil
	}
	return cIds.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Database_Commit
func Java_io_v_syncbase_internal_Database_Commit(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbCommit(cName, cHandle, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_Abort
func Java_io_v_syncbase_internal_Database_Abort(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbAbort(cName, cHandle, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_GetResumeMarker
func Java_io_v_syncbase_internal_Database_GetResumeMarker(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) C.jbyteArray {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var cMarker C.v23_syncbase_Bytes
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbGetResumeMarker(cName, cHandle, &cMarker, &cErr)
	if maybeThrowException(env, &cErr) {
		return nil
	}
	return cMarker.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Database_ListSyncgroups
func Java_io_v_syncbase_internal_Database_ListSyncgroups(env *C.JNIEnv, cls C.jclass, name C.jstring) C.jobject {
	cName := newVStringFromJava(env, name)
	var cIds C.v23_syncbase_Ids
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbListSyncgroups(cName, &cIds, &cErr)
	if maybeThrowException(env, &cErr) {
		return nil
	}
	return cIds.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Database_CreateSyncgroup
func Java_io_v_syncbase_internal_Database_CreateSyncgroup(env *C.JNIEnv, cls C.jclass, name C.jstring, sgId C.jobject, spec C.jobject, info C.jobject) {
	cName := newVStringFromJava(env, name)
	cSgId := newVIdFromJava(env, sgId)
	cSpec := newVSyngroupSpecFromJava(env, spec)
	cMyInfo := newVSyncgroupMemberInfoFromJava(env, info)
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbCreateSyncgroup(cName, cSgId, cSpec, cMyInfo, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_JoinSyncgroup
func Java_io_v_syncbase_internal_Database_JoinSyncgroup(env *C.JNIEnv, cls C.jclass, name C.jstring, remoteSyncbaseName C.jstring, expectedSyncbaseBlessings C.jobject, sgId C.jobject, info C.jobject) C.jobject {
	cName := newVStringFromJava(env, name)
	cRemoteSyncbaseName := newVStringFromJava(env, remoteSyncbaseName)
	cExpectedSyncbaseBlessings := newVStringsFromJava(env, expectedSyncbaseBlessings)
	cSgId := newVIdFromJava(env, sgId)
	cMyInfo := newVSyncgroupMemberInfoFromJava(env, info)
	var cSpec C.v23_syncbase_SyncgroupSpec
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbJoinSyncgroup(cName, cRemoteSyncbaseName, cExpectedSyncbaseBlessings, cSgId, cMyInfo, &cSpec, &cErr)
	maybeThrowException(env, &cErr)
	return cSpec.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Database_LeaveSyncgroup
func Java_io_v_syncbase_internal_Database_LeaveSyncgroup(env *C.JNIEnv, cls C.jclass, name C.jstring, sgId C.jobject) {
	cName := newVStringFromJava(env, name)
	cSgId := newVIdFromJava(env, sgId)
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbLeaveSyncgroup(cName, cSgId, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_DestroySyncgroup
func Java_io_v_syncbase_internal_Database_DestroySyncgroup(env *C.JNIEnv, cls C.jclass, name C.jstring, sgId C.jobject) {
	cName := newVStringFromJava(env, name)
	cSgId := newVIdFromJava(env, sgId)
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbDestroySyncgroup(cName, cSgId, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_EjectFromSyncgroup
func Java_io_v_syncbase_internal_Database_EjectFromSyncgroup(env *C.JNIEnv, cls C.jclass, name C.jstring, sgId C.jobject, member C.jstring) {
	cName := newVStringFromJava(env, name)
	cSgId := newVIdFromJava(env, sgId)
	cMember := newVStringFromJava(env, member)
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbEjectFromSyncgroup(cName, cSgId, cMember, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_GetSyncgroupSpec
func Java_io_v_syncbase_internal_Database_GetSyncgroupSpec(env *C.JNIEnv, cls C.jclass, name C.jstring, sgId C.jobject) C.jobject {
	cName := newVStringFromJava(env, name)
	cSgId := newVIdFromJava(env, sgId)
	var cSpec C.v23_syncbase_SyncgroupSpec
	var cVersion C.v23_syncbase_String
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbGetSyncgroupSpec(cName, cSgId, &cSpec, &cVersion, &cErr)
	if maybeThrowException(env, &cErr) {
		return nil
	}
	obj := C.NewObjectA(env, versionedSyncgroupSpecClass.class, versionedSyncgroupSpecClass.init, nil)
	C.SetObjectField(env, obj, versionedSyncgroupSpecClass.version, cVersion.extractToJava(env))
	C.SetObjectField(env, obj, versionedSyncgroupSpecClass.syncgroupSpec, cSpec.extractToJava(env))
	return obj
}

//export Java_io_v_syncbase_internal_Database_SetSyncgroupSpec
func Java_io_v_syncbase_internal_Database_SetSyncgroupSpec(env *C.JNIEnv, cls C.jclass, name C.jstring, sgId C.jobject, versionedSpec C.jobject) {
	cName := newVStringFromJava(env, name)
	cSgId := newVIdFromJava(env, sgId)
	cSpec, cVersion := newVSyngroupSpecAndVersionFromJava(env, versionedSpec)
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbSetSyncgroupSpec(cName, cSgId, cSpec, cVersion, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Database_GetSyncgroupMembers
func Java_io_v_syncbase_internal_Database_GetSyncgroupMembers(env *C.JNIEnv, cls C.jclass, name C.jstring, sgId C.jobject) C.jobject {
	cName := newVStringFromJava(env, name)
	cSgId := newVIdFromJava(env, sgId)
	var cMembers C.v23_syncbase_SyncgroupMemberInfoMap
	var cErr C.v23_syncbase_VError
	v23_syncbase_DbGetSyncgroupMembers(cName, cSgId, &cMembers, &cErr)
	if maybeThrowException(env, &cErr) {
		return nil
	}
	return cMembers.extractToJava(env)
}

func Java_io_v_syncbase_internal_Database_WatchPatterns(env *C.JNIEnv, cls C.jclass, name C.jstring, resumeMaker C.jbyteArray, patters C.jobject, callbacks C.jobject) {
}

func Java_io_v_syncbase_internal_Collection_GetPermissions(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) C.jobject {
	return nil
}
func Java_io_v_syncbase_internal_Collection_SetPermissions(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring, perms C.jobject) {
}

//export Java_io_v_syncbase_internal_Collection_Create
func Java_io_v_syncbase_internal_Collection_Create(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring, perms C.jobject) {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var cErr C.v23_syncbase_VError
	// TODO(razvanm): construct a proper C.v23_syncbase_Permissions based on
	// the perms object.
	v23_syncbase_CollectionCreate(cName, cHandle, C.v23_syncbase_Permissions{}, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Collection_Destroy
func Java_io_v_syncbase_internal_Collection_Destroy(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var cErr C.v23_syncbase_VError
	v23_syncbase_CollectionDestroy(cName, cHandle, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Collection_Exists
func Java_io_v_syncbase_internal_Collection_Exists(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) C.jboolean {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var r C.v23_syncbase_Bool
	var cErr C.v23_syncbase_VError
	v23_syncbase_CollectionExists(cName, cHandle, &r, &cErr)
	maybeThrowException(env, &cErr)
	return C.jboolean(r)
}

//export Java_io_v_syncbase_internal_Collection_DeleteRange
func Java_io_v_syncbase_internal_Collection_DeleteRange(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring, start C.jbyteArray, limit C.jbyteArray) {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	cStart := newVBytesFromJava(env, start)
	cLimit := newVBytesFromJava(env, limit)
	var cErr C.v23_syncbase_VError
	v23_syncbase_CollectionDeleteRange(cName, cHandle, cStart, cLimit, &cErr)
	maybeThrowException(env, &cErr)
}

func Java_io_v_syncbase_internal_Collection_Scan(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring, start C.jbyteArray, limit C.jbyteArray, callbacks C.jobject) {
}

//export Java_io_v_syncbase_internal_Row_Exists
func Java_io_v_syncbase_internal_Row_Exists(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) C.jboolean {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var r C.v23_syncbase_Bool
	var cErr C.v23_syncbase_VError
	v23_syncbase_RowExists(cName, cHandle, &r, &cErr)
	maybeThrowException(env, &cErr)
	return C.jboolean(r)
}

//export Java_io_v_syncbase_internal_Row_Get
func Java_io_v_syncbase_internal_Row_Get(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) C.jbyteArray {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var r C.v23_syncbase_Bytes
	var cErr C.v23_syncbase_VError
	v23_syncbase_RowGet(cName, cHandle, &r, &cErr)
	maybeThrowException(env, &cErr)
	return r.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Row_Put
func Java_io_v_syncbase_internal_Row_Put(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring, value C.jbyteArray) {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var cErr C.v23_syncbase_VError
	cValue := newVBytesFromJava(env, value)
	v23_syncbase_RowPut(cName, cHandle, cValue, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Row_Delete
func Java_io_v_syncbase_internal_Row_Delete(env *C.JNIEnv, cls C.jclass, name C.jstring, handle C.jstring) {
	cName := newVStringFromJava(env, name)
	cHandle := newVStringFromJava(env, handle)
	var cErr C.v23_syncbase_VError
	v23_syncbase_RowDelete(cName, cHandle, &cErr)
	maybeThrowException(env, &cErr)
}

//export Java_io_v_syncbase_internal_Blessings_DebugString
func Java_io_v_syncbase_internal_Blessings_DebugString(env *C.JNIEnv, cls C.jclass) C.jstring {
	var cDebugString C.v23_syncbase_String
	v23_syncbase_BlessingStoreDebugString(&cDebugString)
	return cDebugString.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Blessings_AppBlessingFromContext
func Java_io_v_syncbase_internal_Blessings_AppBlessingFromContext(env *C.JNIEnv, cls C.jclass) C.jstring {
	var cBlessing C.v23_syncbase_String
	var cErr C.v23_syncbase_VError
	v23_syncbase_AppBlessingFromContext(&cBlessing, &cErr)
	maybeThrowException(env, &cErr)
	return cBlessing.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Blessings_UserBlessingFromContext
func Java_io_v_syncbase_internal_Blessings_UserBlessingFromContext(env *C.JNIEnv, cls C.jclass) C.jstring {
	var cBlessing C.v23_syncbase_String
	var cErr C.v23_syncbase_VError
	v23_syncbase_UserBlessingFromContext(&cBlessing, &cErr)
	maybeThrowException(env, &cErr)
	return cBlessing.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Util_Encode
func Java_io_v_syncbase_internal_Util_Encode(env *C.JNIEnv, cls C.jclass, s C.jstring) C.jstring {
	cPlainStr := newVStringFromJava(env, s)
	var cEncodedStr C.v23_syncbase_String
	v23_syncbase_Encode(cPlainStr, &cEncodedStr)
	return cEncodedStr.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Util_EncodeId
func Java_io_v_syncbase_internal_Util_EncodeId(env *C.JNIEnv, cls C.jclass, obj C.jobject) C.jstring {
	cId := newVIdFromJava(env, obj)
	var cEncodedId C.v23_syncbase_String
	v23_syncbase_EncodeId(cId, &cEncodedId)
	return cEncodedId.extractToJava(env)
}

//export Java_io_v_syncbase_internal_Util_NamingJoin
func Java_io_v_syncbase_internal_Util_NamingJoin(env *C.JNIEnv, cls C.jclass, obj C.jobject) C.jstring {
	cElements := newVStringsFromJava(env, obj)
	var cStr C.v23_syncbase_String
	v23_syncbase_NamingJoin(cElements, &cStr)
	return cStr.extractToJava(env)
}

// The functions below are defined in this file and not in jni_types.go due to
// "inconsistent definitions" errors for various functions (C.NewObjectA and
// C.SetObjectField for example).

// All the extractToJava methods return Java types and deallocate all the
// pointers inside v23_syncbase_* variable.

func (x *C.v23_syncbase_Id) extractToJava(env *C.JNIEnv) C.jobject {
	obj := C.NewObjectA(env, idClass.class, idClass.init, nil)
	C.SetObjectField(env, obj, idClass.blessing, x.blessing.extractToJava(env))
	C.SetObjectField(env, obj, idClass.name, x.name.extractToJava(env))
	x.free()
	return obj
}

// newVIds creates a v23_syncbase_Ids from a List<Id>.
func newVIdsFromJava(env *C.JNIEnv, obj C.jobject) C.v23_syncbase_Ids {
	if obj == nil {
		return C.v23_syncbase_Ids{}
	}
	listInterface := newJListInterface(env, obj)
	iterObj := C.CallObjectMethod(env, obj, listInterface.iterator)
	if C.ExceptionOccurred(env) != nil {
		panic("newVIds exception while trying to call List.iterator()")
	}

	iteratorInterface := newJIteratorInterface(env, iterObj)
	tmp := []C.v23_syncbase_Id{}
	for C.CallBooleanMethodA(env, iterObj, iteratorInterface.hasNext, nil) == C.JNI_TRUE {
		idObj := C.CallObjectMethod(env, iterObj, iteratorInterface.next)
		if C.ExceptionOccurred(env) != nil {
			panic("newVIds exception while trying to call Iterator.next()")
		}
		tmp = append(tmp, newVIdFromJava(env, idObj))
	}

	size := C.size_t(len(tmp)) * C.size_t(C.sizeof_v23_syncbase_Id)
	r := C.v23_syncbase_Ids{
		p: (*C.v23_syncbase_Id)(unsafe.Pointer(C.malloc(size))),
		n: C.int(len(tmp)),
	}
	for i := range tmp {
		*r.at(i) = tmp[i]
	}
	return r
}

func (x *C.v23_syncbase_Ids) extractToJava(env *C.JNIEnv) C.jobject {
	obj := C.NewObjectA(env, arrayListClass.class, arrayListClass.init, nil)
	for i := 0; i < int(x.n); i++ {
		idObj := x.at(i).extractToJava(env)
		arg := *(*C.jvalue)(unsafe.Pointer(&idObj))
		C.CallBooleanMethodA(env, obj, arrayListClass.add, &arg)
	}
	x.free()
	return obj
}

// newVStringsFromJava creates a v23_syncbase_Strings from a List<String>.
func newVStringsFromJava(env *C.JNIEnv, obj C.jobject) C.v23_syncbase_Strings {
	if obj == nil {
		return C.v23_syncbase_Strings{}
	}
	listInterface := newJListInterface(env, obj)
	iterObj := C.CallObjectMethod(env, obj, listInterface.iterator)
	if C.ExceptionOccurred(env) != nil {
		panic("newVStringsFromJava exception while trying to call List.iterator()")
	}

	iteratorInterface := newJIteratorInterface(env, iterObj)
	tmp := []C.v23_syncbase_String{}
	for C.CallBooleanMethodA(env, iterObj, iteratorInterface.hasNext, nil) == C.JNI_TRUE {
		stringObj := C.CallObjectMethod(env, iterObj, iteratorInterface.next)
		if C.ExceptionOccurred(env) != nil {
			panic("newVStringsFromJava exception while trying to call Iterator.next()")
		}
		tmp = append(tmp, newVStringFromJava(env, C.jstring(stringObj)))
	}

	size := C.size_t(len(tmp)) * C.size_t(C.sizeof_v23_syncbase_String)
	r := C.v23_syncbase_Strings{
		p: (*C.v23_syncbase_String)(unsafe.Pointer(C.malloc(size))),
		n: C.int(len(tmp)),
	}
	for i := range tmp {
		*r.at(i) = tmp[i]
	}
	return r
}

func (x *C.v23_syncbase_SyncgroupSpec) extractToJava(env *C.JNIEnv) C.jobject {
	obj := C.NewObjectA(env, syncgroupSpecClass.class, syncgroupSpecClass.init, nil)
	C.SetObjectField(env, obj, syncgroupSpecClass.description, x.description.extractToJava(env))
	C.SetObjectField(env, obj, syncgroupSpecClass.publishSyncbaseName, x.publishSyncbaseName.extractToJava(env))
	// TODO(razvanm): Also extract the permissions.
	C.SetObjectField(env, obj, syncgroupSpecClass.collections, x.collections.extractToJava(env))
	C.SetObjectField(env, obj, syncgroupSpecClass.mountTables, x.mountTables.extractToJava(env))
	C.SetBooleanField(env, obj, syncgroupSpecClass.isPrivate, x.isPrivate.extractToJava())
	return obj
}

func (x *C.v23_syncbase_SyncgroupMemberInfo) extractToJava(env *C.JNIEnv) C.jobject {
	obj := C.NewObjectA(env, syncgroupMemberInfoClass.class, syncgroupMemberInfoClass.init, nil)
	C.SetByteField(env, obj, syncgroupMemberInfoClass.syncPriority, C.jbyte(x.syncPriority))
	C.SetByteField(env, obj, syncgroupMemberInfoClass.blobDevType, C.jbyte(x.blobDevType))
	return obj
}

func (x *C.v23_syncbase_SyncgroupMemberInfoMap) extractToJava(env *C.JNIEnv) C.jobject {
	obj := C.NewObjectA(env, hashMapClass.class, hashMapClass.init, nil)
	for i := 0; i < int(x.n); i++ {
		k, v := x.at(i)
		key := k.extractToJava(env)
		value := v.extractToJava(env)
		args := C.allocJValueArray(2)
		C.setJValueArrayElement(args, 0, *(*C.jvalue)(unsafe.Pointer(&key)))
		C.setJValueArrayElement(args, 1, *(*C.jvalue)(unsafe.Pointer(&value)))
		C.CallObjectMethodA(env, obj, hashMapClass.put, args)
		C.free(unsafe.Pointer(args))
	}
	x.free()
	return obj
}

func (x *C.v23_syncbase_VError) extractToJava(env *C.JNIEnv) C.jobject {
	if x.id.p == nil {
		return nil
	}
	obj := C.NewObjectA(env, verrorClass.class, verrorClass.init, nil)
	C.SetObjectField(env, obj, verrorClass.id, x.id.extractToJava(env))
	C.SetLongField(env, obj, verrorClass.actionCode, C.jlong(x.actionCode))
	C.SetObjectField(env, obj, verrorClass.message, x.msg.extractToJava(env))
	C.SetObjectField(env, obj, verrorClass.stack, x.stack.extractToJava(env))
	x.free()
	return obj
}

func (x *C.v23_syncbase_Strings) extractToJava(env *C.JNIEnv) C.jobject {
	obj := C.NewObjectA(env, arrayListClass.class, arrayListClass.init, nil)
	for i := 0; i < int(x.n); i++ {
		s := x.at(i).extractToJava(env)
		arg := *(*C.jvalue)(unsafe.Pointer(&s))
		C.CallBooleanMethodA(env, obj, arrayListClass.add, &arg)
	}
	x.free()
	return obj
}

// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build java android
// +build cgo

package main

// #include <stdlib.h>
// #include "jni_wrapper.h"
// #include "lib.h"
import "C"

type jArrayListClass struct {
	class C.jclass
	init  C.jmethodID
	add   C.jmethodID
}

func newJArrayListClass(env *C.JNIEnv) jArrayListClass {
	cls, init := initClass(env, "java/util/ArrayList")
	return jArrayListClass{
		class: cls,
		init:  init,
		add:   jGetMethodID(env, cls, "add", "(Ljava/lang/Object;)Z"),
	}
}

type jCollectionRowPattern struct {
	class              C.jclass
	init               C.jmethodID
	collectionBlessing C.jfieldID
	collectionName     C.jfieldID
	rowKey             C.jfieldID
}

func newJCollectionRowPattern(env *C.JNIEnv) jCollectionRowPattern {
	cls, init := initClass(env, "io/v/syncbase/core/CollectionRowPattern")
	return jCollectionRowPattern{
		class:              cls,
		init:               init,
		collectionBlessing: jGetFieldID(env, cls, "collectionBlessing", "Ljava/lang/String;"),
		collectionName:     jGetFieldID(env, cls, "collectionName", "Ljava/lang/String;"),
		rowKey:             jGetFieldID(env, cls, "rowKey", "Ljava/lang/String;"),
	}
}

type jHashMap struct {
	class C.jclass
	init  C.jmethodID
	put   C.jmethodID
}

func newJHashMap(env *C.JNIEnv) jHashMap {
	cls, init := initClass(env, "java/util/HashMap")
	return jHashMap{
		class: cls,
		init:  init,
		put:   jGetMethodID(env, cls, "put", "(Ljava/lang/Object;Ljava/lang/Object;)Ljava/lang/Object;"),
	}
}

type jIdClass struct {
	class    C.jclass
	init     C.jmethodID
	blessing C.jfieldID
	name     C.jfieldID
}

func newJIdClass(env *C.JNIEnv) jIdClass {
	cls, init := initClass(env, "io/v/syncbase/core/Id")
	return jIdClass{
		class:    cls,
		init:     init,
		blessing: jGetFieldID(env, cls, "blessing", "Ljava/lang/String;"),
		name:     jGetFieldID(env, cls, "name", "Ljava/lang/String;"),
	}
}

type jIteratorInterface struct {
	hasNext C.jmethodID
	next    C.jmethodID
}

func newJIteratorInterface(env *C.JNIEnv, obj C.jobject) jIteratorInterface {
	cls := C.GetObjectClass(env, obj)
	return jIteratorInterface{
		hasNext: jGetMethodID(env, cls, "hasNext", "()Z"),
		next:    jGetMethodID(env, cls, "next", "()Ljava/lang/Object;"),
	}
}

type jKeyValue struct {
	class C.jclass
	init  C.jmethodID
	key   C.jfieldID
	value C.jfieldID
}

func newJKeyValue(env *C.JNIEnv) jKeyValue {
	cls, init := initClass(env, "io/v/syncbase/core/KeyValue")
	return jKeyValue{
		class: cls,
		init:  init,
		key:   jGetFieldID(env, cls, "key", "Ljava/lang/String;"),
		value: jGetFieldID(env, cls, "value", "[B"),
	}

}

type jListInterface struct {
	iterator C.jmethodID
	size     C.jmethodID
}

func newJListInterface(env *C.JNIEnv, obj C.jobject) jListInterface {
	cls := C.GetObjectClass(env, obj)
	return jListInterface{
		size:     jGetMethodID(env, cls, "size", "()I"),
		iterator: jGetMethodID(env, cls, "iterator", "()Ljava/util/Iterator;"),
	}
}

type jPermissions struct {
	class C.jclass
	init  C.jmethodID
	json  C.jfieldID
}

func newJPermissions(env *C.JNIEnv) jPermissions {
	cls, init := initClass(env, "io/v/syncbase/core/Permissions")
	return jPermissions{
		class: cls,
		init:  init,
		json:  jGetFieldID(env, cls, "json", "[B"),
	}
}

type jScanCallbacks struct {
	onKeyValue C.jmethodID
	onDone     C.jmethodID
}

func newJScanCallbacks(env *C.JNIEnv, obj C.jobject) jScanCallbacks {
	cls := C.GetObjectClass(env, obj)
	return jScanCallbacks{
		onKeyValue: jGetMethodID(env, cls, "onKeyValue", "(Lio/v/syncbase/core/KeyValue;)V"),
		onDone:     jGetMethodID(env, cls, "onDone", "(Lio/v/syncbase/core/VError;)V"),
	}
}

type jSyncgroupMemberInfo struct {
	class        C.jclass
	init         C.jmethodID
	syncPriority C.jfieldID
	blobDevType  C.jfieldID
}

func newJSyncgroupMemberInfo(env *C.JNIEnv) jSyncgroupMemberInfo {
	cls, init := initClass(env, "io/v/syncbase/core/SyncgroupMemberInfo")
	return jSyncgroupMemberInfo{
		class:        cls,
		init:         init,
		syncPriority: jGetFieldID(env, cls, "syncPriority", "I"),
		blobDevType:  jGetFieldID(env, cls, "blobDevType", "I"),
	}
}

type jSyncgroupSpec struct {
	class               C.jclass
	init                C.jmethodID
	description         C.jfieldID
	publishSyncbaseName C.jfieldID
	permissions         C.jfieldID
	collections         C.jfieldID
	mountTables         C.jfieldID
	isPrivate           C.jfieldID
}

func newJSyncgroupSpec(env *C.JNIEnv) jSyncgroupSpec {
	cls, init := initClass(env, "io/v/syncbase/core/SyncgroupSpec")
	return jSyncgroupSpec{
		class:               cls,
		init:                init,
		description:         jGetFieldID(env, cls, "description", "Ljava/lang/String;"),
		publishSyncbaseName: jGetFieldID(env, cls, "publishSyncbaseName", "Ljava/lang/String;"),
		permissions:         jGetFieldID(env, cls, "permissions", "Lio/v/syncbase/core/Permissions;"),
		collections:         jGetFieldID(env, cls, "collections", "Ljava/util/List;"),
		mountTables:         jGetFieldID(env, cls, "mountTables", "Ljava/util/List;"),
		isPrivate:           jGetFieldID(env, cls, "isPrivate", "Z"),
	}
}

type jVErrorClass struct {
	class      C.jclass
	init       C.jmethodID
	id         C.jfieldID
	actionCode C.jfieldID
	message    C.jfieldID
	stack      C.jfieldID
}

func newJVErrorClass(env *C.JNIEnv) jVErrorClass {
	cls, init := initClass(env, "io/v/syncbase/core/VError")
	return jVErrorClass{
		class:      cls,
		init:       init,
		id:         jGetFieldID(env, cls, "id", "Ljava/lang/String;"),
		actionCode: jGetFieldID(env, cls, "actionCode", "J"),
		message:    jGetFieldID(env, cls, "message", "Ljava/lang/String;"),
		stack:      jGetFieldID(env, cls, "stack", "Ljava/lang/String;"),
	}
}

type jVersionedPermissions struct {
	class       C.jclass
	init        C.jmethodID
	version     C.jfieldID
	permissions C.jfieldID
}

func newJVersionedPermissions(env *C.JNIEnv) jVersionedPermissions {
	cls, init := initClass(env, "io/v/syncbase/core/VersionedPermissions")
	return jVersionedPermissions{
		class:       cls,
		init:        init,
		version:     jGetFieldID(env, cls, "version", "Ljava/lang/String;"),
		permissions: jGetFieldID(env, cls, "permissions", "Lio/v/syncbase/core/Permissions;"),
	}
}

type jVersionedSyncgroupSpec struct {
	class         C.jclass
	init          C.jmethodID
	version       C.jfieldID
	syncgroupSpec C.jfieldID
}

func newJVersionedSyncgroupSpec(env *C.JNIEnv) jVersionedSyncgroupSpec {
	cls, init := initClass(env, "io/v/syncbase/core/VersionedSyncgroupSpec")
	return jVersionedSyncgroupSpec{
		class:         cls,
		init:          init,
		version:       jGetFieldID(env, cls, "version", "Ljava/lang/String;"),
		syncgroupSpec: jGetFieldID(env, cls, "syncgroupSpec", "Lio/v/syncbase/core/SyncgroupSpec;"),
	}
}

type jWatchChange struct {
	class        C.jclass
	init         C.jmethodID
	collection   C.jfieldID
	row          C.jfieldID
	changeType   C.jfieldID
	value        C.jfieldID
	resumeMarker C.jfieldID
	fromSync     C.jfieldID
	continued    C.jfieldID
}

func newJWatchChange(env *C.JNIEnv) jWatchChange {
	cls, init := initClass(env, "io/v/syncbase/core/WatchChange")
	return jWatchChange{
		class:        cls,
		init:         init,
		collection:   jGetFieldID(env, cls, "collection", "Lio/v/syncbase/core/Id;"),
		row:          jGetFieldID(env, cls, "row", "Ljava/lang/String;"),
		changeType:   jGetFieldID(env, cls, "changeType", "Lio/v/syncbase/core/WatchChange$ChangeType;"),
		value:        jGetFieldID(env, cls, "value", "[B"),
		resumeMarker: jGetFieldID(env, cls, "resumeMarker", "Ljava/lang/String;"),
		fromSync:     jGetFieldID(env, cls, "fromSync", "Z"),
		continued:    jGetFieldID(env, cls, "continued", "Z"),
	}
}

type jWatchPatternsCallbacks struct {
	onChange C.jmethodID
	onError  C.jmethodID
}

func newJWatchPatternsCallbacks(env *C.JNIEnv, obj C.jobject) jWatchPatternsCallbacks {
	cls := C.GetObjectClass(env, obj)
	return jWatchPatternsCallbacks{
		onChange: jGetMethodID(env, cls, "onChange", "(Lio/v/syncbase/core/WatchChange;)V"),
		onError:  jGetMethodID(env, cls, "onError", "(Lio/v/syncbase/core/VError;)V"),
	}
}

// initClass returns the jclass and the jmethodID of the default constructor for
// a class.
func initClass(env *C.JNIEnv, name string) (C.jclass, C.jmethodID) {
	cls, err := jFindClass(env, name)
	if err != nil {
		// The invariant is that we only deal with classes that must be
		// known to the JVM. A panic indicates a bug in our code.
		panic(err)
	}
	init := jGetMethodID(env, cls, "<init>", "()V")
	return cls, init
}

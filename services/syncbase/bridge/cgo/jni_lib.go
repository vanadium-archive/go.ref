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

type jIdClass struct {
	class    C.jclass
	init     C.jmethodID
	blessing C.jfieldID
	name     C.jfieldID
}

func newJIdClass(env *C.JNIEnv) jIdClass {
	cls, init := initClass(env, "io/v/syncbase/internal/Id")
	return jIdClass{
		class:    cls,
		init:     init,
		blessing: jGetFieldID(env, cls, "blessing", "Ljava/lang/String;"),
		name:     jGetFieldID(env, cls, "name", "Ljava/lang/String;"),
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

type jSyncgroupMemberInfo struct {
	class        C.jclass
	init         C.jmethodID
	syncPriority C.jfieldID
	blobDevType  C.jfieldID
}

func newJSyncgroupMemberInfo(env *C.JNIEnv) jSyncgroupMemberInfo {
	cls, init := initClass(env, "io/v/syncbase/internal/Database$SyncgroupMemberInfo")
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
	cls, init := initClass(env, "io/v/syncbase/internal/Database$SyncgroupSpec")
	return jSyncgroupSpec{
		class:               cls,
		init:                init,
		description:         jGetFieldID(env, cls, "description", "Ljava/lang/String;"),
		publishSyncbaseName: jGetFieldID(env, cls, "publishSyncbaseName", "Ljava/lang/String;"),
		permissions:         jGetFieldID(env, cls, "permissions", "Lio/v/syncbase/internal/Permissions;"),
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
	cls, init := initClass(env, "io/v/syncbase/internal/VError")
	return jVErrorClass{
		class:      cls,
		init:       init,
		id:         jGetFieldID(env, cls, "id", "Ljava/lang/String;"),
		actionCode: jGetFieldID(env, cls, "actionCode", "J"),
		message:    jGetFieldID(env, cls, "message", "Ljava/lang/String;"),
		stack:      jGetFieldID(env, cls, "stack", "Ljava/lang/String;"),
	}
}

type jVersionedSyncgroupSpec struct {
	class         C.jclass
	init          C.jmethodID
	version       C.jfieldID
	syncgroupSpec C.jfieldID
}

func newJVersionedSyncgroupSpec(env *C.JNIEnv) jVersionedSyncgroupSpec {
	cls, init := initClass(env, "io/v/syncbase/internal/Database$VersionedSyncgroupSpec")
	return jVersionedSyncgroupSpec{
		class:         cls,
		init:          init,
		version:       jGetFieldID(env, cls, "version", "Ljava/lang/String;"),
		syncgroupSpec: jGetFieldID(env, cls, "syncgroupSpec", "Lio/v/syncbase/internal/Database$SyncgroupSpec;"),
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

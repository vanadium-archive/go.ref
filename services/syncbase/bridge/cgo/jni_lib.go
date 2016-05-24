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
}

func (c *jArrayListClass) Init(env *C.JNIEnv) error {
	var err error
	c.class, c.init, err = initClass(env, "java/util/ArrayList")
	if err != nil {
		return err
	}
	return nil
}

type jIdClass struct {
	class C.jclass
	init  C.jmethodID
}

func (c *jIdClass) Init(env *C.JNIEnv) error {
	var err error
	c.class, c.init, err = initClass(env, "io/v/syncbase/internal/Id")
	if err != nil {
		return err
	}
	return nil
}

type jVErrorClass struct {
	class      C.jclass
	init       C.jmethodID
	id         C.jfieldID
	actionCode C.jfieldID
	message    C.jfieldID
	stack      C.jfieldID
}

func (c *jVErrorClass) Init(env *C.JNIEnv) error {
	var err error
	c.class, c.init, err = initClass(env, "io/v/syncbase/internal/VError")
	if err != nil {
		return err
	}
	c.id, err = JGetFieldID(env, c.class, "id", "Ljava/lang/String;")
	if err != nil {
		return err
	}
	c.actionCode, err = JGetFieldID(env, c.class, "actionCode", "J")
	if err != nil {
		return err
	}
	c.message, err = JGetFieldID(env, c.class, "message", "Ljava/lang/String;")
	if err != nil {
		return err
	}
	c.stack, err = JGetFieldID(env, c.class, "stack", "Ljava/lang/String;")
	if err != nil {
		return err
	}
	return nil
}

// initClass returns the jclass and the jmethodID of the default constructor for
// a class.
func initClass(env *C.JNIEnv, name string) (C.jclass, C.jmethodID, error) {
	cls, err := JFindClass(env, name)
	if err != nil {
		return nil, nil, err
	}
	init, err := JGetMethodID(env, cls, "<init>", "()V")
	if err != nil {
		return nil, nil, err
	}
	return cls, init, nil
}

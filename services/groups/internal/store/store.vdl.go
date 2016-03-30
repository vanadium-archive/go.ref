// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: store

package store

import (
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/verror"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Error definitions
var (

	// KeyExists means the given key already exists in the store.
	ErrKeyExists = verror.Register("v.io/x/ref/services/groups/internal/store.KeyExists", verror.NoRetry, "{1:}{2:} Key exists{:_}")
	// UnknownKey means the given key does not exist in the store.
	ErrUnknownKey = verror.Register("v.io/x/ref/services/groups/internal/store.UnknownKey", verror.NoRetry, "{1:}{2:} Unknown key{:_}")
)

// NewErrKeyExists returns an error with the ErrKeyExists ID.
func NewErrKeyExists(ctx *context.T) error {
	return verror.New(ErrKeyExists, ctx)
}

// NewErrUnknownKey returns an error with the ErrUnknownKey ID.
func NewErrUnknownKey(ctx *context.T) error {
	return verror.New(ErrUnknownKey, ctx)
}

var __VDLInitCalled bool

// __VDLInit performs vdl initialization.  It is safe to call multiple times.
// If you have an init ordering issue, just insert the following line verbatim
// into your source files in this package, right after the "package foo" clause:
//
//    var _ = __VDLInit()
//
// The purpose of this function is to ensure that vdl initialization occurs in
// the right order, and very early in the init sequence.  In particular, vdl
// registration and package variable initialization needs to occur before
// functions like vdl.TypeOf will work properly.
//
// This function returns a dummy value, so that it can be used to initialize the
// first var in the file, to take advantage of Go's defined init order.
func __VDLInit() struct{} {
	if __VDLInitCalled {
		return struct{}{}
	}
	__VDLInitCalled = true

	// Set error format strings.
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(ErrKeyExists.ID), "{1:}{2:} Key exists{:_}")
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(ErrUnknownKey.ID), "{1:}{2:} Unknown key{:_}")

	return struct{}{}
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Source: nativetest.vdl

// Package nativetest tests a package with native type conversions.
package nativetest

import (
	// VDL system imports
	"v.io/v23/vdl"

	// VDL user imports
	"time"
	"v.io/v23/vdl/testdata/nativetest"
)

type WireString int32

func (WireString) __VDLReflect(struct {
	Name string "v.io/x/ref/lib/vdl/testdata/nativetest.WireString"
}) {
}

type WireMapStringInt int32

func (WireMapStringInt) __VDLReflect(struct {
	Name string "v.io/x/ref/lib/vdl/testdata/nativetest.WireMapStringInt"
}) {
}

type WireTime int32

func (WireTime) __VDLReflect(struct {
	Name string "v.io/x/ref/lib/vdl/testdata/nativetest.WireTime"
}) {
}

type WireSamePkg int32

func (WireSamePkg) __VDLReflect(struct {
	Name string "v.io/x/ref/lib/vdl/testdata/nativetest.WireSamePkg"
}) {
}

type WireMultiImport int32

func (WireMultiImport) __VDLReflect(struct {
	Name string "v.io/x/ref/lib/vdl/testdata/nativetest.WireMultiImport"
}) {
}

type WireRenameMe int32

func (WireRenameMe) __VDLReflect(struct {
	Name string "v.io/x/ref/lib/vdl/testdata/nativetest.WireRenameMe"
}) {
}

type WireAll struct {
	A string
	B map[string]int
	C time.Time
	D nativetest.NativeSamePkg
	E map[nativetest.NativeSamePkg]time.Time
	F WireRenameMe
}

func (WireAll) __VDLReflect(struct {
	Name string "v.io/x/ref/lib/vdl/testdata/nativetest.WireAll"
}) {
}

func init() {
	vdl.RegisterNative(wireMapStringIntToNative, wireMapStringIntFromNative)
	vdl.RegisterNative(wireMultiImportToNative, wireMultiImportFromNative)
	vdl.RegisterNative(wireSamePkgToNative, wireSamePkgFromNative)
	vdl.RegisterNative(wireStringToNative, wireStringFromNative)
	vdl.RegisterNative(wireTimeToNative, wireTimeFromNative)
	vdl.Register((*WireString)(nil))
	vdl.Register((*WireMapStringInt)(nil))
	vdl.Register((*WireTime)(nil))
	vdl.Register((*WireSamePkg)(nil))
	vdl.Register((*WireMultiImport)(nil))
	vdl.Register((*WireRenameMe)(nil))
	vdl.Register((*WireAll)(nil))
}

// Type-check WireMapStringInt conversion functions.
var _ func(WireMapStringInt, *map[string]int) error = wireMapStringIntToNative
var _ func(*WireMapStringInt, map[string]int) error = wireMapStringIntFromNative

// Type-check WireMultiImport conversion functions.
var _ func(WireMultiImport, *map[nativetest.NativeSamePkg]time.Time) error = wireMultiImportToNative
var _ func(*WireMultiImport, map[nativetest.NativeSamePkg]time.Time) error = wireMultiImportFromNative

// Type-check WireSamePkg conversion functions.
var _ func(WireSamePkg, *nativetest.NativeSamePkg) error = wireSamePkgToNative
var _ func(*WireSamePkg, nativetest.NativeSamePkg) error = wireSamePkgFromNative

// Type-check WireString conversion functions.
var _ func(WireString, *string) error = wireStringToNative
var _ func(*WireString, string) error = wireStringFromNative

// Type-check WireTime conversion functions.
var _ func(WireTime, *time.Time) error = wireTimeToNative
var _ func(*WireTime, time.Time) error = wireTimeFromNative

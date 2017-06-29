// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: main

package main

import (
	"fmt"
	"v.io/v23/vdl"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Type definitions

type dataRep int

const (
	dataRepHex dataRep = iota
	dataRepBinary
)

// dataRepAll holds all labels for dataRep.
var dataRepAll = [...]dataRep{dataRepHex, dataRepBinary}

// dataRepFromString creates a dataRep from a string label.
func dataRepFromString(label string) (x dataRep, err error) {
	err = x.Set(label)
	return
}

// Set assigns label to x.
func (x *dataRep) Set(label string) error {
	switch label {
	case "Hex", "hex":
		*x = dataRepHex
		return nil
	case "Binary", "binary":
		*x = dataRepBinary
		return nil
	}
	*x = -1
	return fmt.Errorf("unknown label %q in main.dataRep", label)
}

// String returns the string label of x.
func (x dataRep) String() string {
	switch x {
	case dataRepHex:
		return "Hex"
	case dataRepBinary:
		return "Binary"
	}
	return ""
}

func (dataRep) VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/cmd/vom.dataRep"`
	Enum struct{ Hex, Binary string }
}) {
}

func (x dataRep) VDLIsZero() bool {
	return x == dataRepHex
}

func (x dataRep) VDLWrite(enc vdl.Encoder) error {
	if err := enc.WriteValueString(__VDLType_enum_1, x.String()); err != nil {
		return err
	}
	return nil
}

func (x *dataRep) VDLRead(dec vdl.Decoder) error {
	switch value, err := dec.ReadValueString(); {
	case err != nil:
		return err
	default:
		if err := x.Set(value); err != nil {
			return err
		}
	}
	return nil
}

// Hold type definitions in package-level variables, for better performance.
var (
	__VDLType_enum_1 *vdl.Type
)

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

	// Register types.
	vdl.Register((*dataRep)(nil))

	// Initialize type definitions.
	__VDLType_enum_1 = vdl.TypeOf((*dataRep)(nil))

	return struct{}{}
}

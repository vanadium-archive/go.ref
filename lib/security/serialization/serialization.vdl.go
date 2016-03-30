// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: serialization

package serialization

import (
	"fmt"
	"v.io/v23/security"
	"v.io/v23/vdl"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Type definitions

type SignedHeader struct {
	ChunkSizeBytes int64
}

func (SignedHeader) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/lib/security/serialization.SignedHeader"`
}) {
}

func (m *SignedHeader) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	fieldsTarget1, err := t.StartFields(tt)
	if err != nil {
		return err
	}
	keyTarget2, fieldTarget3, err := fieldsTarget1.StartField("ChunkSizeBytes")
	if err != vdl.ErrFieldNoExist && err != nil {
		return err
	}
	if err != vdl.ErrFieldNoExist {

		var4 := (m.ChunkSizeBytes == int64(0))
		if var4 {
			if err := fieldTarget3.FromZero(tt.NonOptional().Field(0).Type); err != nil {
				return err
			}
		} else {
			if err := fieldTarget3.FromInt(int64(m.ChunkSizeBytes), tt.NonOptional().Field(0).Type); err != nil {
				return err
			}
		}
		if err := fieldsTarget1.FinishField(keyTarget2, fieldTarget3); err != nil {
			return err
		}
	}
	if err := t.FinishFields(fieldsTarget1); err != nil {
		return err
	}
	return nil
}

func (m *SignedHeader) MakeVDLTarget() vdl.Target {
	return &SignedHeaderTarget{Value: m}
}

type SignedHeaderTarget struct {
	Value                *SignedHeader
	chunkSizeBytesTarget vdl.Int64Target
	vdl.TargetBase
	vdl.FieldsTargetBase
}

func (t *SignedHeaderTarget) StartFields(tt *vdl.Type) (vdl.FieldsTarget, error) {

	if ttWant := vdl.TypeOf((*SignedHeader)(nil)).Elem(); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	return t, nil
}
func (t *SignedHeaderTarget) StartField(name string) (key, field vdl.Target, _ error) {
	switch name {
	case "ChunkSizeBytes":
		t.chunkSizeBytesTarget.Value = &t.Value.ChunkSizeBytes
		target, err := &t.chunkSizeBytesTarget, error(nil)
		return nil, target, err
	default:
		return nil, nil, fmt.Errorf("field %s not in struct v.io/x/ref/lib/security/serialization.SignedHeader", name)
	}
}
func (t *SignedHeaderTarget) FinishField(_, _ vdl.Target) error {
	return nil
}
func (t *SignedHeaderTarget) FinishFields(_ vdl.FieldsTarget) error {

	return nil
}
func (t *SignedHeaderTarget) FromZero(tt *vdl.Type) error {
	*t.Value = SignedHeader{}
	return nil
}

type HashCode [32]byte

func (HashCode) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/lib/security/serialization.HashCode"`
}) {
}

func (m *HashCode) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	if err := t.FromBytes([]byte((*m)[:]), tt); err != nil {
		return err
	}
	return nil
}

func (m *HashCode) MakeVDLTarget() vdl.Target {
	return &HashCodeTarget{Value: m}
}

type HashCodeTarget struct {
	Value *HashCode
	vdl.TargetBase
}

func (t *HashCodeTarget) FromBytes(src []byte, tt *vdl.Type) error {

	if ttWant := vdl.TypeOf((*HashCode)(nil)); !vdl.Compatible(tt, ttWant) {
		return fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	copy((*t.Value)[:], src)

	return nil
}
func (t *HashCodeTarget) FromZero(tt *vdl.Type) error {
	*t.Value = HashCode{}
	return nil
}

type (
	// SignedData represents any single field of the SignedData union type.
	//
	// SignedData describes the information sent by a SigningWriter and read by VerifiyingReader.
	SignedData interface {
		// Index returns the field index.
		Index() int
		// Interface returns the field value as an interface.
		Interface() interface{}
		// Name returns the field name.
		Name() string
		// __VDLReflect describes the SignedData union type.
		__VDLReflect(__SignedDataReflect)
		FillVDLTarget(vdl.Target, *vdl.Type) error
	}
	// SignedDataSignature represents field Signature of the SignedData union type.
	SignedDataSignature struct{ Value security.Signature }
	// SignedDataHash represents field Hash of the SignedData union type.
	SignedDataHash struct{ Value HashCode }
	// __SignedDataReflect describes the SignedData union type.
	__SignedDataReflect struct {
		Name               string `vdl:"v.io/x/ref/lib/security/serialization.SignedData"`
		Type               SignedData
		UnionTargetFactory signedDataTargetFactory
		Union              struct {
			Signature SignedDataSignature
			Hash      SignedDataHash
		}
	}
)

func (x SignedDataSignature) Index() int                       { return 0 }
func (x SignedDataSignature) Interface() interface{}           { return x.Value }
func (x SignedDataSignature) Name() string                     { return "Signature" }
func (x SignedDataSignature) __VDLReflect(__SignedDataReflect) {}

func (m SignedDataSignature) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	fieldsTarget1, err := t.StartFields(tt)
	if err != nil {
		return err
	}
	keyTarget2, fieldTarget3, err := fieldsTarget1.StartField("Signature")
	if err != nil {
		return err
	}

	if err := m.Value.FillVDLTarget(fieldTarget3, tt.NonOptional().Field(0).Type); err != nil {
		return err
	}
	if err := fieldsTarget1.FinishField(keyTarget2, fieldTarget3); err != nil {
		return err
	}
	if err := t.FinishFields(fieldsTarget1); err != nil {
		return err
	}

	return nil
}

func (m SignedDataSignature) MakeVDLTarget() vdl.Target {
	return nil
}

func (x SignedDataHash) Index() int                       { return 1 }
func (x SignedDataHash) Interface() interface{}           { return x.Value }
func (x SignedDataHash) Name() string                     { return "Hash" }
func (x SignedDataHash) __VDLReflect(__SignedDataReflect) {}

func (m SignedDataHash) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	fieldsTarget1, err := t.StartFields(tt)
	if err != nil {
		return err
	}
	keyTarget2, fieldTarget3, err := fieldsTarget1.StartField("Hash")
	if err != nil {
		return err
	}

	if err := m.Value.FillVDLTarget(fieldTarget3, tt.NonOptional().Field(1).Type); err != nil {
		return err
	}
	if err := fieldsTarget1.FinishField(keyTarget2, fieldTarget3); err != nil {
		return err
	}
	if err := t.FinishFields(fieldsTarget1); err != nil {
		return err
	}

	return nil
}

func (m SignedDataHash) MakeVDLTarget() vdl.Target {
	return nil
}

type SignedDataTarget struct {
	Value     *SignedData
	fieldName string

	vdl.TargetBase
	vdl.FieldsTargetBase
}

func (t *SignedDataTarget) StartFields(tt *vdl.Type) (vdl.FieldsTarget, error) {
	if ttWant := vdl.TypeOf((*SignedData)(nil)); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}

	return t, nil
}
func (t *SignedDataTarget) StartField(name string) (key, field vdl.Target, _ error) {
	t.fieldName = name
	switch name {
	case "Signature":
		val := security.Signature{}
		return nil, &security.SignatureTarget{Value: &val}, nil
	case "Hash":
		val := HashCode{}
		return nil, &HashCodeTarget{Value: &val}, nil
	default:
		return nil, nil, fmt.Errorf("field %s not in union v.io/x/ref/lib/security/serialization.SignedData", name)
	}
}
func (t *SignedDataTarget) FinishField(_, fieldTarget vdl.Target) error {
	switch t.fieldName {
	case "Signature":
		*t.Value = SignedDataSignature{*(fieldTarget.(*security.SignatureTarget)).Value}
	case "Hash":
		*t.Value = SignedDataHash{*(fieldTarget.(*HashCodeTarget)).Value}
	}
	return nil
}
func (t *SignedDataTarget) FinishFields(_ vdl.FieldsTarget) error {

	return nil
}
func (t *SignedDataTarget) FromZero(tt *vdl.Type) error {
	*t.Value = SignedData(SignedDataSignature{})
	return nil
}

type signedDataTargetFactory struct{}

func (t signedDataTargetFactory) VDLMakeUnionTarget(union interface{}) (vdl.Target, error) {
	if typedUnion, ok := union.(*SignedData); ok {
		return &SignedDataTarget{Value: typedUnion}, nil
	}
	return nil, fmt.Errorf("got %T, want *SignedData", union)
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

	// Register types.
	vdl.Register((*SignedHeader)(nil))
	vdl.Register((*HashCode)(nil))
	vdl.Register((*SignedData)(nil))

	return struct{}{}
}

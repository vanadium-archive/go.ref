// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: server

package server

import (
	"fmt"
	"v.io/v23/security/access"
	"v.io/v23/services/groups"
	"v.io/v23/vdl"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Type definitions

// groupData represents the persistent state of a group. (The group name is
// persisted as the store entry key.)
type groupData struct {
	Perms   access.Permissions
	Entries map[groups.BlessingPatternChunk]struct{}
}

func (groupData) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/services/groups/internal/server.groupData"`
}) {
}

func (m *groupData) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	fieldsTarget1, err := t.StartFields(tt)
	if err != nil {
		return err
	}
	var var4 bool
	if len(m.Perms) == 0 {
		var4 = true
	}
	if var4 {
		if err := fieldsTarget1.ZeroField("Perms"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget2, fieldTarget3, err := fieldsTarget1.StartField("Perms")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			if err := m.Perms.FillVDLTarget(fieldTarget3, tt.NonOptional().Field(0).Type); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget2, fieldTarget3); err != nil {
				return err
			}
		}
	}
	var var7 bool
	if len(m.Entries) == 0 {
		var7 = true
	}
	if var7 {
		if err := fieldsTarget1.ZeroField("Entries"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget5, fieldTarget6, err := fieldsTarget1.StartField("Entries")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			setTarget8, err := fieldTarget6.StartSet(tt.NonOptional().Field(1).Type, len(m.Entries))
			if err != nil {
				return err
			}
			for key10 := range m.Entries {
				keyTarget9, err := setTarget8.StartKey()
				if err != nil {
					return err
				}

				if err := key10.FillVDLTarget(keyTarget9, tt.NonOptional().Field(1).Type.Key()); err != nil {
					return err
				}
				if err := setTarget8.FinishKey(keyTarget9); err != nil {
					return err
				}
			}
			if err := fieldTarget6.FinishSet(setTarget8); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget5, fieldTarget6); err != nil {
				return err
			}
		}
	}
	if err := t.FinishFields(fieldsTarget1); err != nil {
		return err
	}
	return nil
}

func (m *groupData) MakeVDLTarget() vdl.Target {
	return &groupDataTarget{Value: m}
}

type groupDataTarget struct {
	Value         *groupData
	permsTarget   access.PermissionsTarget
	entriesTarget __VDLTarget1_set
	vdl.TargetBase
	vdl.FieldsTargetBase
}

func (t *groupDataTarget) StartFields(tt *vdl.Type) (vdl.FieldsTarget, error) {

	if ttWant := vdl.TypeOf((*groupData)(nil)).Elem(); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	return t, nil
}
func (t *groupDataTarget) StartField(name string) (key, field vdl.Target, _ error) {
	switch name {
	case "Perms":
		t.permsTarget.Value = &t.Value.Perms
		target, err := &t.permsTarget, error(nil)
		return nil, target, err
	case "Entries":
		t.entriesTarget.Value = &t.Value.Entries
		target, err := &t.entriesTarget, error(nil)
		return nil, target, err
	default:
		return nil, nil, fmt.Errorf("field %s not in struct v.io/x/ref/services/groups/internal/server.groupData", name)
	}
}
func (t *groupDataTarget) FinishField(_, _ vdl.Target) error {
	return nil
}
func (t *groupDataTarget) ZeroField(name string) error {
	switch name {
	case "Perms":
		t.Value.Perms = access.Permissions(nil)
		return nil
	case "Entries":
		t.Value.Entries = map[groups.BlessingPatternChunk]struct{}(nil)
		return nil
	default:
		return fmt.Errorf("field %s not in struct v.io/x/ref/services/groups/internal/server.groupData", name)
	}
}
func (t *groupDataTarget) FinishFields(_ vdl.FieldsTarget) error {

	return nil
}

// map[groups.BlessingPatternChunk]struct{}
type __VDLTarget1_set struct {
	Value     *map[groups.BlessingPatternChunk]struct{}
	currKey   groups.BlessingPatternChunk
	keyTarget groups.BlessingPatternChunkTarget
	vdl.TargetBase
	vdl.SetTargetBase
}

func (t *__VDLTarget1_set) StartSet(tt *vdl.Type, len int) (vdl.SetTarget, error) {

	if ttWant := vdl.TypeOf((*map[groups.BlessingPatternChunk]struct{})(nil)); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	*t.Value = make(map[groups.BlessingPatternChunk]struct{})
	return t, nil
}
func (t *__VDLTarget1_set) StartKey() (key vdl.Target, _ error) {
	t.currKey = groups.BlessingPatternChunk("")
	t.keyTarget.Value = &t.currKey
	target, err := &t.keyTarget, error(nil)
	return target, err
}
func (t *__VDLTarget1_set) FinishKey(key vdl.Target) error {
	(*t.Value)[t.currKey] = struct{}{}
	return nil
}
func (t *__VDLTarget1_set) FinishSet(list vdl.SetTarget) error {
	if len(*t.Value) == 0 {
		*t.Value = nil
	}

	return nil
}

func (x *groupData) VDLRead(dec vdl.Decoder) error {
	*x = groupData{}
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible struct %T, from %v", *x, dec.Type())
	}
	for {
		f, err := dec.NextField()
		if err != nil {
			return err
		}
		switch f {
		case "":
			return dec.FinishValue()
		case "Perms":
			if err = x.Perms.VDLRead(dec); err != nil {
				return err
			}
		case "Entries":
			if err = __VDLRead1_set(dec, &x.Entries); err != nil {
				return err
			}
		default:
			if err = dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

func __VDLRead1_set(dec vdl.Decoder, x *map[groups.BlessingPatternChunk]struct{}) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible set %T, from %v", *x, dec.Type())
	}
	var tmpMap map[groups.BlessingPatternChunk]struct{}
	if len := dec.LenHint(); len > 0 {
		tmpMap = make(map[groups.BlessingPatternChunk]struct{}, len)
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			*x = tmpMap
			return dec.FinishValue()
		}
		var key groups.BlessingPatternChunk
		{
			if err = key.VDLRead(dec); err != nil {
				return err
			}
		}
		if tmpMap == nil {
			tmpMap = make(map[groups.BlessingPatternChunk]struct{})
		}
		tmpMap[key] = struct{}{}
	}
}

func (x groupData) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(vdl.TypeOf((*groupData)(nil)).Elem()); err != nil {
		return err
	}
	var var1 bool
	if len(x.Perms) == 0 {
		var1 = true
	}
	if !(var1) {
		if err := enc.NextField("Perms"); err != nil {
			return err
		}
		if err := x.Perms.VDLWrite(enc); err != nil {
			return err
		}
	}
	var var2 bool
	if len(x.Entries) == 0 {
		var2 = true
	}
	if !(var2) {
		if err := enc.NextField("Entries"); err != nil {
			return err
		}
		if err := __VDLWrite1_set(enc, &x.Entries); err != nil {
			return err
		}
	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

func __VDLWrite1_set(enc vdl.Encoder, x *map[groups.BlessingPatternChunk]struct{}) error {
	if err := enc.StartValue(vdl.TypeOf((*map[groups.BlessingPatternChunk]struct{})(nil))); err != nil {
		return err
	}
	for key := range *x {
		if err := key.VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
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
	vdl.Register((*groupData)(nil))

	return struct{}{}
}

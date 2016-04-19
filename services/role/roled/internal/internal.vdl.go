// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Package: internal

package internal

import (
	"fmt"
	"v.io/v23/security"
	"v.io/v23/uniqueid"
	"v.io/v23/vdl"
)

var _ = __VDLInit() // Must be first; see __VDLInit comments for details.

//////////////////////////////////////////////////
// Type definitions

// Config contains the attributes of the role, and the list of members who have
// access to it.
type Config struct {
	// List of role objects, relative to this role, from which to import
	// the set of members. File path notation like "." and ".." may be used.
	// The set of members who have access to this role is the union of this
	// role's members and those of all the imported roles.
	ImportMembers []string
	// Blessings that match at least one of the patterns in this set are
	// allowed to act on behalf of the role.
	Members []security.BlessingPattern
	// Indicates that the blessing name of the caller should be appended to
	// the role blessing name.
	Extend bool
	// If Audit is true, each use of the role blessing will be reported to
	// an auditing service and will be usable only if the report was
	// successful.
	Audit bool
	// The amount of time for which the role blessing will be valid. It is a
	// string representation of a time.Duration, e.g. "24h". An empty string
	// indicates that the role blessing will not expire.
	Expiry string
	// The blessings issued for this role will only be valid for
	// communicating with peers that match at least one of these patterns.
	// If the list is empty, all peers are allowed.
	Peers []security.BlessingPattern
}

func (Config) __VDLReflect(struct {
	Name string `vdl:"v.io/x/ref/services/role/roled/internal.Config"`
}) {
}

func (m *Config) FillVDLTarget(t vdl.Target, tt *vdl.Type) error {
	fieldsTarget1, err := t.StartFields(tt)
	if err != nil {
		return err
	}
	var var4 bool
	if len(m.ImportMembers) == 0 {
		var4 = true
	}
	if var4 {
		if err := fieldsTarget1.ZeroField("ImportMembers"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget2, fieldTarget3, err := fieldsTarget1.StartField("ImportMembers")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			listTarget5, err := fieldTarget3.StartList(tt.NonOptional().Field(0).Type, len(m.ImportMembers))
			if err != nil {
				return err
			}
			for i, elem7 := range m.ImportMembers {
				elemTarget6, err := listTarget5.StartElem(i)
				if err != nil {
					return err
				}
				if err := elemTarget6.FromString(string(elem7), tt.NonOptional().Field(0).Type.Elem()); err != nil {
					return err
				}
				if err := listTarget5.FinishElem(elemTarget6); err != nil {
					return err
				}
			}
			if err := fieldTarget3.FinishList(listTarget5); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget2, fieldTarget3); err != nil {
				return err
			}
		}
	}
	var var10 bool
	if len(m.Members) == 0 {
		var10 = true
	}
	if var10 {
		if err := fieldsTarget1.ZeroField("Members"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget8, fieldTarget9, err := fieldsTarget1.StartField("Members")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			listTarget11, err := fieldTarget9.StartList(tt.NonOptional().Field(1).Type, len(m.Members))
			if err != nil {
				return err
			}
			for i, elem13 := range m.Members {
				elemTarget12, err := listTarget11.StartElem(i)
				if err != nil {
					return err
				}

				if err := elem13.FillVDLTarget(elemTarget12, tt.NonOptional().Field(1).Type.Elem()); err != nil {
					return err
				}
				if err := listTarget11.FinishElem(elemTarget12); err != nil {
					return err
				}
			}
			if err := fieldTarget9.FinishList(listTarget11); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget8, fieldTarget9); err != nil {
				return err
			}
		}
	}
	var16 := (m.Extend == false)
	if var16 {
		if err := fieldsTarget1.ZeroField("Extend"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget14, fieldTarget15, err := fieldsTarget1.StartField("Extend")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}
			if err := fieldTarget15.FromBool(bool(m.Extend), tt.NonOptional().Field(2).Type); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget14, fieldTarget15); err != nil {
				return err
			}
		}
	}
	var19 := (m.Audit == false)
	if var19 {
		if err := fieldsTarget1.ZeroField("Audit"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget17, fieldTarget18, err := fieldsTarget1.StartField("Audit")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}
			if err := fieldTarget18.FromBool(bool(m.Audit), tt.NonOptional().Field(3).Type); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget17, fieldTarget18); err != nil {
				return err
			}
		}
	}
	var22 := (m.Expiry == "")
	if var22 {
		if err := fieldsTarget1.ZeroField("Expiry"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget20, fieldTarget21, err := fieldsTarget1.StartField("Expiry")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}
			if err := fieldTarget21.FromString(string(m.Expiry), tt.NonOptional().Field(4).Type); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget20, fieldTarget21); err != nil {
				return err
			}
		}
	}
	var var25 bool
	if len(m.Peers) == 0 {
		var25 = true
	}
	if var25 {
		if err := fieldsTarget1.ZeroField("Peers"); err != nil && err != vdl.ErrFieldNoExist {
			return err
		}
	} else {
		keyTarget23, fieldTarget24, err := fieldsTarget1.StartField("Peers")
		if err != vdl.ErrFieldNoExist {
			if err != nil {
				return err
			}

			listTarget26, err := fieldTarget24.StartList(tt.NonOptional().Field(5).Type, len(m.Peers))
			if err != nil {
				return err
			}
			for i, elem28 := range m.Peers {
				elemTarget27, err := listTarget26.StartElem(i)
				if err != nil {
					return err
				}

				if err := elem28.FillVDLTarget(elemTarget27, tt.NonOptional().Field(5).Type.Elem()); err != nil {
					return err
				}
				if err := listTarget26.FinishElem(elemTarget27); err != nil {
					return err
				}
			}
			if err := fieldTarget24.FinishList(listTarget26); err != nil {
				return err
			}
			if err := fieldsTarget1.FinishField(keyTarget23, fieldTarget24); err != nil {
				return err
			}
		}
	}
	if err := t.FinishFields(fieldsTarget1); err != nil {
		return err
	}
	return nil
}

func (m *Config) MakeVDLTarget() vdl.Target {
	return &ConfigTarget{Value: m}
}

type ConfigTarget struct {
	Value               *Config
	importMembersTarget vdl.StringSliceTarget
	membersTarget       __VDLTarget1_list
	extendTarget        vdl.BoolTarget
	auditTarget         vdl.BoolTarget
	expiryTarget        vdl.StringTarget
	peersTarget         __VDLTarget1_list
	vdl.TargetBase
	vdl.FieldsTargetBase
}

func (t *ConfigTarget) StartFields(tt *vdl.Type) (vdl.FieldsTarget, error) {

	if ttWant := vdl.TypeOf((*Config)(nil)).Elem(); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	return t, nil
}
func (t *ConfigTarget) StartField(name string) (key, field vdl.Target, _ error) {
	switch name {
	case "ImportMembers":
		t.importMembersTarget.Value = &t.Value.ImportMembers
		target, err := &t.importMembersTarget, error(nil)
		return nil, target, err
	case "Members":
		t.membersTarget.Value = &t.Value.Members
		target, err := &t.membersTarget, error(nil)
		return nil, target, err
	case "Extend":
		t.extendTarget.Value = &t.Value.Extend
		target, err := &t.extendTarget, error(nil)
		return nil, target, err
	case "Audit":
		t.auditTarget.Value = &t.Value.Audit
		target, err := &t.auditTarget, error(nil)
		return nil, target, err
	case "Expiry":
		t.expiryTarget.Value = &t.Value.Expiry
		target, err := &t.expiryTarget, error(nil)
		return nil, target, err
	case "Peers":
		t.peersTarget.Value = &t.Value.Peers
		target, err := &t.peersTarget, error(nil)
		return nil, target, err
	default:
		return nil, nil, fmt.Errorf("field %s not in struct v.io/x/ref/services/role/roled/internal.Config", name)
	}
}
func (t *ConfigTarget) FinishField(_, _ vdl.Target) error {
	return nil
}
func (t *ConfigTarget) ZeroField(name string) error {
	switch name {
	case "ImportMembers":
		t.Value.ImportMembers = []string(nil)
		return nil
	case "Members":
		t.Value.Members = []security.BlessingPattern(nil)
		return nil
	case "Extend":
		t.Value.Extend = false
		return nil
	case "Audit":
		t.Value.Audit = false
		return nil
	case "Expiry":
		t.Value.Expiry = ""
		return nil
	case "Peers":
		t.Value.Peers = []security.BlessingPattern(nil)
		return nil
	default:
		return fmt.Errorf("field %s not in struct v.io/x/ref/services/role/roled/internal.Config", name)
	}
}
func (t *ConfigTarget) FinishFields(_ vdl.FieldsTarget) error {

	return nil
}

// []security.BlessingPattern
type __VDLTarget1_list struct {
	Value      *[]security.BlessingPattern
	elemTarget security.BlessingPatternTarget
	vdl.TargetBase
	vdl.ListTargetBase
}

func (t *__VDLTarget1_list) StartList(tt *vdl.Type, len int) (vdl.ListTarget, error) {

	if ttWant := vdl.TypeOf((*[]security.BlessingPattern)(nil)); !vdl.Compatible(tt, ttWant) {
		return nil, fmt.Errorf("type %v incompatible with %v", tt, ttWant)
	}
	if cap(*t.Value) < len {
		*t.Value = make([]security.BlessingPattern, len)
	} else {
		*t.Value = (*t.Value)[:len]
	}
	return t, nil
}
func (t *__VDLTarget1_list) StartElem(index int) (elem vdl.Target, _ error) {
	t.elemTarget.Value = &(*t.Value)[index]
	target, err := &t.elemTarget, error(nil)
	return target, err
}
func (t *__VDLTarget1_list) FinishElem(elem vdl.Target) error {
	return nil
}
func (t *__VDLTarget1_list) FinishList(elem vdl.ListTarget) error {

	return nil
}

func (x *Config) VDLRead(dec vdl.Decoder) error {
	*x = Config{}
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
		case "ImportMembers":
			if err = __VDLRead1_list(dec, &x.ImportMembers); err != nil {
				return err
			}
		case "Members":
			if err = __VDLRead2_list(dec, &x.Members); err != nil {
				return err
			}
		case "Extend":
			if err = dec.StartValue(); err != nil {
				return err
			}
			if x.Extend, err = dec.DecodeBool(); err != nil {
				return err
			}
			if err = dec.FinishValue(); err != nil {
				return err
			}
		case "Audit":
			if err = dec.StartValue(); err != nil {
				return err
			}
			if x.Audit, err = dec.DecodeBool(); err != nil {
				return err
			}
			if err = dec.FinishValue(); err != nil {
				return err
			}
		case "Expiry":
			if err = dec.StartValue(); err != nil {
				return err
			}
			if x.Expiry, err = dec.DecodeString(); err != nil {
				return err
			}
			if err = dec.FinishValue(); err != nil {
				return err
			}
		case "Peers":
			if err = __VDLRead2_list(dec, &x.Peers); err != nil {
				return err
			}
		default:
			if err = dec.SkipValue(); err != nil {
				return err
			}
		}
	}
}

func __VDLRead1_list(dec vdl.Decoder, x *[]string) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible list %T, from %v", *x, dec.Type())
	}
	switch len := dec.LenHint(); {
	case len > 0:
		*x = make([]string, 0, len)
	default:
		*x = nil
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			return dec.FinishValue()
		}
		var elem string
		if err = dec.StartValue(); err != nil {
			return err
		}
		if elem, err = dec.DecodeString(); err != nil {
			return err
		}
		if err = dec.FinishValue(); err != nil {
			return err
		}
		*x = append(*x, elem)
	}
}

func __VDLRead2_list(dec vdl.Decoder, x *[]security.BlessingPattern) error {
	var err error
	if err = dec.StartValue(); err != nil {
		return err
	}
	if (dec.StackDepth() == 1 || dec.IsAny()) && !vdl.Compatible(vdl.TypeOf(*x), dec.Type()) {
		return fmt.Errorf("incompatible list %T, from %v", *x, dec.Type())
	}
	switch len := dec.LenHint(); {
	case len > 0:
		*x = make([]security.BlessingPattern, 0, len)
	default:
		*x = nil
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			return dec.FinishValue()
		}
		var elem security.BlessingPattern
		if err = elem.VDLRead(dec); err != nil {
			return err
		}
		*x = append(*x, elem)
	}
}

func (x Config) VDLWrite(enc vdl.Encoder) error {
	if err := enc.StartValue(vdl.TypeOf((*Config)(nil)).Elem()); err != nil {
		return err
	}
	var var1 bool
	if len(x.ImportMembers) == 0 {
		var1 = true
	}
	if !(var1) {
		if err := enc.NextField("ImportMembers"); err != nil {
			return err
		}
		if err := __VDLWrite1_list(enc, &x.ImportMembers); err != nil {
			return err
		}
	}
	var var2 bool
	if len(x.Members) == 0 {
		var2 = true
	}
	if !(var2) {
		if err := enc.NextField("Members"); err != nil {
			return err
		}
		if err := __VDLWrite2_list(enc, &x.Members); err != nil {
			return err
		}
	}
	var3 := (x.Extend == false)
	if !(var3) {
		if err := enc.NextField("Extend"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.TypeOf((*bool)(nil))); err != nil {
			return err
		}
		if err := enc.EncodeBool(x.Extend); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	var4 := (x.Audit == false)
	if !(var4) {
		if err := enc.NextField("Audit"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.TypeOf((*bool)(nil))); err != nil {
			return err
		}
		if err := enc.EncodeBool(x.Audit); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	var5 := (x.Expiry == "")
	if !(var5) {
		if err := enc.NextField("Expiry"); err != nil {
			return err
		}
		if err := enc.StartValue(vdl.TypeOf((*string)(nil))); err != nil {
			return err
		}
		if err := enc.EncodeString(x.Expiry); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	var var6 bool
	if len(x.Peers) == 0 {
		var6 = true
	}
	if !(var6) {
		if err := enc.NextField("Peers"); err != nil {
			return err
		}
		if err := __VDLWrite2_list(enc, &x.Peers); err != nil {
			return err
		}
	}
	if err := enc.NextField(""); err != nil {
		return err
	}
	return enc.FinishValue()
}

func __VDLWrite1_list(enc vdl.Encoder, x *[]string) error {
	if err := enc.StartValue(vdl.TypeOf((*[]string)(nil))); err != nil {
		return err
	}
	for i := 0; i < len(*x); i++ {
		if err := enc.StartValue(vdl.TypeOf((*string)(nil))); err != nil {
			return err
		}
		if err := enc.EncodeString((*x)[i]); err != nil {
			return err
		}
		if err := enc.FinishValue(); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

func __VDLWrite2_list(enc vdl.Encoder, x *[]security.BlessingPattern) error {
	if err := enc.StartValue(vdl.TypeOf((*[]security.BlessingPattern)(nil))); err != nil {
		return err
	}
	for i := 0; i < len(*x); i++ {
		if err := (*x)[i].VDLWrite(enc); err != nil {
			return err
		}
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}
	return enc.FinishValue()
}

//////////////////////////////////////////////////
// Const definitions

// LoggingCaveat is a caveat that will always validate but it logs the parameter on every attempt to validate it.
var LoggingCaveat = security.CaveatDescriptor{
	Id: uniqueid.Id{
		176,
		52,
		28,
		237,
		226,
		223,
		129,
		189,
		237,
		112,
		151,
		187,
		85,
		173,
		128,
		0,
	},
	ParamType: vdl.TypeOf((*[]string)(nil)),
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
	vdl.Register((*Config)(nil))

	return struct{}{}
}

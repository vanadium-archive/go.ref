// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"
	"strings"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/vdlutil"
)

// genBasicTargetDef generate's a Target definition that involves simple
// assignment.
func genBasicTargetDef(data *goData, t *vdl.Type, X, fromType string) string {
	valueAssn, _, valueFieldDef := genValueField(data, t)
	return fmt.Sprintf(`
type %[1]s struct {
	%[8]s
	%[5]s
}
func (t *%[1]s) From%[6]s(src %[7]s, tt *%[4]sType) error {
	%[11]s
	%[3]s
	%[9]s = %[2]s(src)
	%[10]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[4]sType) error {
	*t.Value = %[12]s
	return nil
}`, targetTypeName(data, t), typeGo(data, t), genIncompatibleTypeCheck(data, t, 0), data.Pkg("v.io/v23/vdl"), targetBaseRef(data, "Target"), X, fromType, valueFieldDef, valueAssn, genWireToNativeConversion(data, t), genResetWire(data, t), typedConst(data, vdl.ZeroValue(t)))
}

// genNumberTargetDef generates a Target that assigns to a number and
// performs numeric conversions.
func genNumberTargetDef(data *goData, t *vdl.Type) string {
	valueAssn, _, valueFieldDef := genValueField(data, t)
	s := fmt.Sprintf(`
type %[1]s struct {
	%[2]s
	%[3]s
}`, targetTypeName(data, t), valueFieldDef, targetBaseRef(data, "Target"))
	s += genNumberFromX(data, t, vdl.Uint64, valueAssn)
	s += genNumberFromX(data, t, vdl.Int64, valueAssn)
	s += genNumberFromX(data, t, vdl.Float64, valueAssn)
	s += fmt.Sprintf(`
func (t *%[1]s) FromZero(tt *%[2]sType) error {
	*t.Value = %[3]s
	return nil
}`, targetTypeName(data, t), data.Pkg("v.io/v23/vdl"), typedConst(data, vdl.ZeroValue(t)))
	return s
}

// genNumberFromX generates a FromX method, e.g. FromInt()
func genNumberFromX(data *goData, targetType *vdl.Type, sourceKind vdl.Kind, valueAssn string) string {
	var X string
	var fromType string
	switch sourceKind {
	case vdl.Uint64:
		X = "Uint"
		fromType = "uint64"
	case vdl.Int64:
		X = "Int"
		fromType = "int64"
	case vdl.Float64:
		X = "Float"
		fromType = "float64"
	default:
		panic("invalid source kind")
	}
	return fmt.Sprintf(`
	func (t *%s) From%s(src %s, tt *%sType) error {
		%s
		%s
		%s
		return nil
	}`, targetTypeName(data, targetType), X, fromType, data.Pkg("v.io/v23/vdl"), genResetWire(data, targetType), genNumberConversion(data, targetType, sourceKind, valueAssn), genWireToNativeConversion(data, targetType))
}

// genNumberConversion generates the code lines needed to perform number conversion.
func genNumberConversion(data *goData, targetType *vdl.Type, sourceKind vdl.Kind, valueAssn string) string {
	targetKind := targetType.Kind()
	targetKindName := vdlutil.FirstRuneToUpper(targetKind.String())
	if targetKindName == "Byte" {
		targetKindName = "Uint8"
	}
	sourceKindName := vdlutil.FirstRuneToUpper(sourceKind.String())
	targetTypeName := typeGo(data, targetType)

	if targetKind == sourceKind {
		return fmt.Sprintf(`%s = %s(src)`, valueAssn, targetTypeName)
	} else {
		return fmt.Sprintf(`val, err := %s%sTo%s(src)
	if err != nil {
		return err
	}
	%s = %s(val)`, data.Pkg("v.io/v23/vdl/vdlconv"), sourceKindName, targetKindName, valueAssn, targetTypeName)
	}
}

// genBytesDef generates a Target for a named []byte type.
func genBytesDef(data *goData, t *vdl.Type) string {
	valueAssn, _, valueFieldDef := genValueField(data, t)
	if t.Kind() == vdl.List {
		return fmt.Sprintf(`
type %[1]s struct {
	%[2]s
	%[5]s
}
func (t *%[1]s) FromBytes(src []byte, tt *%[4]sType) error {
	%[8]s
	%[3]s
	if len(src) == 0 {
		%[6]s = nil
	} else {
		%[6]s = make([]byte, len(src))
		copy(%[6]s, src)
	}
	%[7]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[4]sType) error {
	*t.Value = %[9]s
	return nil
}`, targetTypeName(data, t), valueFieldDef, genIncompatibleTypeCheck(data, t, 0), data.Pkg("v.io/v23/vdl"), targetBaseRef(data, "Target"), valueAssn, genWireToNativeConversion(data, t), genResetWire(data, t), typedConst(data, vdl.ZeroValue(t)))
	} else {
		return fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[5]s
}
func (t *%[1]s) FromBytes(src []byte, tt *%[4]sType) error {
	%[8]s
	%[3]s
	copy((%[6]s)[:], src)
	%[7]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[4]sType) error {
	*t.Value = %[9]s
	return nil
}`, targetTypeName(data, t), typeGo(data, t), genIncompatibleTypeCheck(data, t, 0), data.Pkg("v.io/v23/vdl"), targetBaseRef(data, "Target"), valueAssn, genWireToNativeConversion(data, t), genResetWire(data, t), typedConst(data, vdl.ZeroValue(t)))
	}
}

// genStructTargetDef generates a Target that assigns to a struct
func genStructTargetDef(data *goData, t *vdl.Type) string {
	if t.Name() == "" {
		panic("only named structs supported in generator")
	}
	valueAssn, valueReg, valueFieldDef := genValueField(data, t)
	var additionalBodies string
	s := fmt.Sprintf(`
type %[1]s struct {
	%[2]s`, targetTypeName(data, t), valueFieldDef)
	for i := 0; i < t.NumField(); i++ {
		fld := t.Field(i)
		s += fmt.Sprintf("\n%s", inlineTargetField(data, fld.Type, t, vdlutil.FirstRuneToLower(fld.Name)+"Target"))
	}
	s += fmt.Sprintf(`
	%[2]s
	%[3]s
}
func (t *%[1]s) StartFields(tt *%[4]sType) (%[4]sFieldsTarget, error) {
	%[6]s
	%[5]s
	return t, nil
}
func (t *%[1]s) StartField(name string) (key, field %[4]sTarget, _ error) {
	switch name {`, targetTypeName(data, t), targetBaseRef(data, "Target"), targetBaseRef(data, "FieldsTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1), genResetWire(data, t))
	for i := 0; i < t.NumField(); i++ {
		fld := t.Field(i)
		call, body := createTargetCall(data, fld.Type, t, fmt.Sprintf("t.%sTarget", vdlutil.FirstRuneToLower(fld.Name)), fmt.Sprintf("&%s.%s", valueReg, fld.Name))
		additionalBodies += body
		s += fmt.Sprintf(`
	case %q:
		%s
		return nil, target, err`, fld.Name, call)
	}
	s += fmt.Sprintf(`
	default:
		return nil, nil, %sErrorf("field %%s not in struct %s", name)
	}
}`, data.Pkg("fmt"), t.Name())
	s += fmt.Sprintf(`
func (t *%[1]s) FinishField(_, _ %[2]sTarget) error {
	return nil
}
func (t *%[1]s) FinishFields(_ %[2]sFieldsTarget) error {
	%[5]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[2]sType) error {
	*t.Value = %[4]s
	return nil
}`, targetTypeName(data, t), data.Pkg("v.io/v23/vdl"), valueAssn, typedConst(data, vdl.ZeroValue(t)), genWireToNativeConversion(data, t))
	s += additionalBodies
	return s
}

// genOptionalStructTargetDef generates a Target that assigns to an optional
// struct - that is either calls FromZero or StartFields.
func genOptionalStructTargetDef(data *goData, t *vdl.Type) string {
	valueAssn, _, valueFieldDef := genValueField(data, t)
	call, body := createTargetCall(data, t.Elem(), t, "t.elemTarget", valueAssn)
	s := fmt.Sprintf(`
// Optional %[11]s
type %[1]s struct {
	%[2]s
	%[10]s
	%[3]s
	%[4]s
}
func (t *%[1]s) StartFields(tt *%[5]sType) (%[5]sFieldsTarget, error) {
	%[12]s
	if %[8]s == nil {
		%[8]s = &%[7]s
	}
	%[6]s
	if err != nil {
		return nil, err
	}
	return target.StartFields(tt)
}
func (t *%[1]s) FinishFields(_ %[5]sFieldsTarget) error {
	%[9]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[5]sType) error {
	*t.Value = %[13]s
	return nil
}`, targetTypeName(data, t), valueFieldDef, targetBaseRef(data, "Target"), targetBaseRef(data, "FieldsTarget"), data.Pkg("v.io/v23/vdl"), call, typedConst(data, vdl.ZeroValue(t.Elem())), valueAssn, genWireToNativeConversion(data, t), inlineTargetField(data, t.Elem(), t, "elemTarget"), typeGo(data, t.Elem()), genResetWire(data, t), typedConst(data, vdl.ZeroValue(t)))
	s += body
	return s
}

// genUnionTargetDef generates a Target that assigns to a union.
func genUnionTargetDef(data *goData, t *vdl.Type) string {
	def := data.Env.FindTypeDef(t)
	valueAssn, _, valueFieldDef := genValueField(data, t)
	var s string
	if t.Name() == "" {
		s += fmt.Sprintf("\n// %s", typeGo(data, t))
	}
	var anyValueField = ""
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Type == vdl.AnyType {
			if shouldUseVdlValueForAny(data.Package) {
				anyValueField = fmt.Sprintf("anyValue %sValue", data.Pkg("v.io/v23/vdl"))
			} else {
				anyValueField = fmt.Sprintf("anyValue %sRawBytes", data.Pkg("v.io/v23/vom"))
			}
			break
		}
	}
	s += fmt.Sprintf(`
type %[1]s struct {
	%[7]s
	fieldName string
	%[8]s
	%[2]s
	%[3]s
}
func (t *%[1]s) StartFields(tt *%[4]sType) (%[4]sFieldsTarget, error) {
	%[5]s
	%[6]s
	return t, nil
}
func (t *%[1]s) StartField(name string) (key, field %[4]sTarget, _ error) {
	t.fieldName = name
	switch name {`, targetTypeName(data, t), targetBaseRef(data, "Target"), targetBaseRef(data, "FieldsTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1), genResetWire(data, t), valueFieldDef, anyValueField)
	var additionalBodies string
	for i := 0; i < t.NumField(); i++ {
		fld := t.Field(i)
		if fld.Type == vdl.AnyType {
			if shouldUseVdlValueForAny(data.Package) {
				s += fmt.Sprintf(`
		case %[1]q:
	t.anyValue = %[2]sValue{}
	target, err := %[2]sValueTarget(&t.anyValue)
	return nil, target, err`, fld.Name, data.Pkg("v.io/v23/vdl"))
			} else {
				s += fmt.Sprintf(`
		case %[1]q:
	t.anyValue = %[2]sRawBytes{}
	return nil, t.anyValue.MakeVDLTarget(), nil`, fld.Name, data.Pkg("v.io/v23/vom"))
			}
		} else {
			ref, body := genTargetRef(data, fld.Type)
			additionalBodies += body
			s += fmt.Sprintf(`
		case %[1]q:
	val := %[2]s
	return nil, &%[3]s{Value:&val}, nil`, fld.Name, typedConst(data, vdl.ZeroValue(fld.Type)), ref)
		}
	}
	s += fmt.Sprintf(`
	default:
		return nil, nil, %[4]sErrorf("field %%s not in union %[2]s", name)
	}
}
func (t *%[1]s) FinishField(_, fieldTarget %[3]sTarget) error {
	switch t.fieldName {`, targetTypeName(data, t), t.Name(), data.Pkg("v.io/v23/vdl"), data.Pkg("fmt"))
	for i := 0; i < t.NumField(); i++ {
		fld := t.Field(i)
		if fld.Type == vdl.AnyType {
			s += fmt.Sprintf(`
		case %[1]q:
	%[2]s = %[3]s%[4]s{&t.anyValue}`, fld.Name, valueAssn, def.Name, fld.Name)
		} else {
			s += fmt.Sprintf(`
		case %[1]q:
	%[3]s = %[4]s%[5]s{*(fieldTarget.(*%[2]s)).Value}`, fld.Name, targetTypeName(data, fld.Type), valueAssn, def.Name, fld.Name)
		}
	}
	s += fmt.Sprintf(`}
	return nil
}
func (t *%[1]s) FinishFields(_ %[3]sFieldsTarget) error {
	%[2]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[3]sType) error {
	*t.Value = %[7]s
	return nil
}

type %[6]sFactory struct{}

func (t %[6]sFactory) VDLMakeUnionTarget(union interface{}) (%[3]sTarget, error) {
	if typedUnion, ok := union.(*%[4]s); ok {
		return &%[1]s{Value: typedUnion}, nil
	}
	return nil, %[5]sErrorf("got %%T, want *%[4]s", union)
}`, targetTypeName(data, t), genWireToNativeConversion(data, t), data.Pkg("v.io/v23/vdl"), typeGo(data, t), data.Pkg("fmt"), vdlutil.FirstRuneToLower(targetTypeName(data, t)), typedConst(data, vdl.ZeroValue(t)))
	s += additionalBodies
	return s
}

// genListTargetDef generates a Target that assigns to a list or array
func genListTargetDef(data *goData, t *vdl.Type) string {
	valueAssn, _, valueFieldDef := genValueField(data, t)
	s := fmt.Sprintf(`
// %[8]s
type %[1]s struct {
	%[2]s
	%[7]s
	%[3]s
	%[4]s
}
func (t *%[1]s) StartList(tt *%[5]sType, len int) (%[5]sListTarget, error) {
	%[9]s
	%[6]s`, targetTypeName(data, t), valueFieldDef, targetBaseRef(data, "Target"), targetBaseRef(data, "ListTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1), inlineTargetField(data, t.Elem(), t, "elemTarget"), typeGo(data, t), genResetWire(data, t))
	if t.Kind() == vdl.List {
		s += fmt.Sprintf(`
	if cap(%[1]s) < len {
		%[1]s = make(%[2]s, len)
	} else {
		%[1]s = (%[1]s)[:len]
	}`, valueAssn, typeGo(data, t))
	}
	call, body := createTargetCall(data, t.Elem(), t, "t.elemTarget", fmt.Sprintf("&(%s)[index]", valueAssn))
	s += fmt.Sprintf(`
	return t, nil
}
func (t *%[1]s) StartElem(index int) (elem %[2]sTarget, _ error) {
	%[3]s
	return target, err
}
func (t *%[1]s) FinishElem(elem %[2]sTarget) error {
	return nil
}
func (t *%[1]s) FinishList(elem %[2]sListTarget) error {
	%[4]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[2]sType) error {
	*t.Value = %[6]s
	return nil
}`, targetTypeName(data, t), data.Pkg("v.io/v23/vdl"), call, genWireToNativeConversion(data, t), valueAssn, typedConst(data, vdl.ZeroValue(t)))
	s += body
	return s
}

// genSetTargetDef generates a Target that assigns to a set.
func genSetTargetDef(data *goData, t *vdl.Type) string {
	valueAssn, _, valueFieldDef := genValueField(data, t)
	call, body := createTargetCall(data, t.Key(), t, fmt.Sprintf("t.keyTarget"), "&t.currKey")
	var s string
	if t.Name() == "" {
		s += fmt.Sprintf("\n// %s", typeGo(data, t))
	}
	s += fmt.Sprintf(`
type %[1]s struct {
	%[10]s
	currKey %[3]s
	%[13]s
	%[4]s
	%[5]s
}
func (t *%[1]s) StartSet(tt *%[6]sType, len int) (%[6]sSetTarget, error) {
	%[14]s
	%[7]s
	%[11]s = make(%[2]s)
	return t, nil
}
func (t *%[1]s) StartKey() (key %[6]sTarget, _ error) {
	%[9]s
	%[8]s
	return target, err
}
func (t *%[1]s) FinishKey(key %[6]sTarget) error {
	(%[11]s)[t.currKey] = struct{}{}
	return nil
}
func (t *%[1]s) FinishSet(list %[6]sSetTarget) error {
	if len(%[11]s) == 0 {
		%[11]s = nil
	}
	%[12]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[6]sType) error {
	*t.Value = %[15]s
	return nil
}`, targetTypeName(data, t), typeGo(data, t), typeGo(data, t.Key()), targetBaseRef(data, "Target"), targetBaseRef(data, "SetTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1), call, genResetValue(data, t.Key(), "t.currKey"), valueFieldDef, valueAssn, genWireToNativeConversion(data, t), inlineTargetField(data, t.Key(), t, "keyTarget"), genResetWire(data, t), typedConst(data, vdl.ZeroValue(t)))
	s += body
	return s
}

// genMapTargetDef generates a Target that assigns to a map.
func genMapTargetDef(data *goData, t *vdl.Type) string {
	valueAssn, _, valueFieldDef := genValueField(data, t)
	keyCall, keyBody := createTargetCall(data, t.Key(), t, "t.keyTarget", "&t.currKey")
	elemCall, elemBody := createTargetCall(data, t.Elem(), t, "t.elemTarget", "&t.currElem")
	var s string
	if t.Name() == "" {
		s += fmt.Sprintf("\n// %s", typeGo(data, t))
	}
	s += fmt.Sprintf(`
type %[1]s struct {
	%[13]s
	currKey %[3]s
	currElem %[4]s
	%[16]s
	%[17]s
	%[5]s
	%[6]s
}
func (t *%[1]s) StartMap(tt *%[7]sType, len int) (%[7]sMapTarget, error) {
	%[18]s
	%[8]s
	%[14]s = make(%[2]s)
	return t, nil
}
func (t *%[1]s) StartKey() (key %[7]sTarget, _ error) {
	%[11]s
	%[9]s
	return target, err
}
func (t *%[1]s) FinishKeyStartField(key %[7]sTarget) (field %[7]sTarget, _ error) {
	%[12]s
	%[10]s
	return target, err
}
func (t *%[1]s) FinishField(key, field %[7]sTarget) error {
	(%[14]s)[t.currKey] = t.currElem
	return nil
}
func (t *%[1]s) FinishMap(elem %[7]sMapTarget) error {
	if len(%[14]s) == 0 {
		%[14]s = nil
	}
	%[15]s
	return nil
}
func (t *%[1]s) FromZero(tt *%[7]sType) error {
	*t.Value = %[19]s
	return nil
}`, targetTypeName(data, t), typeGo(data, t), typeGo(data, t.Key()), typeGo(data, t.Elem()), targetBaseRef(data, "Target"), targetBaseRef(data, "MapTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1), keyCall, elemCall, genResetValue(data, t.Key(), "t.currKey"), genResetValue(data, t.Elem(), "t.currElem"), valueFieldDef, valueAssn, genWireToNativeConversion(data, t), inlineTargetField(data, t.Key(), t, "keyTarget"), inlineTargetField(data, t.Elem(), t, "elemTarget"), genResetWire(data, t), typedConst(data, vdl.ZeroValue(t)))
	s += keyBody
	s += elemBody
	return s
}

// genEnumTargetDef generates a Target that assigns to an enum,
func genEnumTargetDef(data *goData, t *vdl.Type) string {
	valueAssn, _, valueFieldDef := genValueField(data, t)
	def := data.Env.FindTypeDef(t)
	s := fmt.Sprintf(`
type %[1]s struct {
	%[2]s
	%[3]s
}
func (t *%[1]s) FromEnumLabel(src string, tt *%[4]sType) error {
	%[6]s
	%[5]s
	switch src {`, targetTypeName(data, t), valueFieldDef, targetBaseRef(data, "Target"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 0), genResetWire(data, t))
	for i := 0; i < t.NumEnumLabel(); i++ {
		s += fmt.Sprintf(`
	case %q:
		%s = %d`, t.EnumLabel(i), valueAssn, i)
	}
	s += fmt.Sprintf(`
	default:
		return %[5]sErrorf("label %%s not in enum %[6]s", src)
	}
	%[1]s
	return nil
}
func (t *%[2]s) FromZero(tt *%[3]sType) error {
	*t.Value = %[4]s
	return nil
}`, genWireToNativeConversion(data, t), targetTypeName(data, t), data.Pkg("v.io/v23/vdl"), typedConst(data, vdl.ZeroValue(t)), data.Pkg("fmt"), def.Name)
	return s
}

// genTargetDef calls the appropriate Target generator based on type t.
func genTargetDef(data *goData, t *vdl.Type) string {
	if !data.createdTargets[t] {
		data.createdTargets[t] = true
		if t.IsBytes() {
			return genBytesDef(data, t)
		}
		switch t.Kind() {
		case vdl.Struct:
			return genStructTargetDef(data, t)
		case vdl.Optional:
			if t.Elem().Kind() != vdl.Struct {
				panic("only structs can be optional")
			}
			return genOptionalStructTargetDef(data, t)
		case vdl.Union:
			return genUnionTargetDef(data, t)
		case vdl.List, vdl.Array:
			return genListTargetDef(data, t)
		case vdl.Set:
			return genSetTargetDef(data, t)
		case vdl.Map:
			return genMapTargetDef(data, t)
		case vdl.Enum:
			return genEnumTargetDef(data, t)
		case vdl.Bool:
			return genBasicTargetDef(data, t, "Bool", "bool")
		case vdl.String:
			return genBasicTargetDef(data, t, "String", "string")
		case vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64, vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64, vdl.Float32, vdl.Float64:
			return genNumberTargetDef(data, t)
		}
	}
	return ""
}

// genTargetRef returns a string representing a the name of a Target for the provided type.
// If the Target body must be generated, it is returned as the second parameter.
func genTargetRef(data *goData, t *vdl.Type) (targTypeName, body string) {
	externalName := externalTargetTypeName(data, t)
	if externalName != "" {
		return externalName, ""
	}

	body = genTargetDef(data, t)
	targTypeName = targetTypeName(data, t)
	return
}

// targetTypeName generates a stable name for a Target for the specified type.
func targetTypeName(data *goData, t *vdl.Type) string {
	if externalName := externalTargetTypeName(data, t); externalName != "" {
		return externalName
	}
	if def := data.Env.FindTypeDef(t); def != nil {
		return def.Name + "Target"
	}
	id := data.unnamedTargets[t]
	if id == 0 {
		id = len(data.unnamedTargets) + 1
		data.unnamedTargets[t] = id
	}
	return fmt.Sprintf("__VDLTarget%d_%s", id, t.Kind())
}

// externalTargetTypeName generates a stable name for a Target for the specified type.
// It differs from targetTypeName in that it returns empty string if the target is
// defined in this package (not external).
func externalTargetTypeName(data *goData, t *vdl.Type) string {
	anon := t.Name() == ""
	switch {
	case t == vdl.ErrorType:
		return data.Pkg("v.io/v23/verror") + "ErrorTarget"
	case anon && t.IsBytes() && t.Kind() == vdl.List:
		return data.Pkg("v.io/v23/vdl") + "BytesTarget"
	case anon && t.Kind() == vdl.List && t.Elem() == vdl.StringType:
		return data.Pkg("v.io/v23/vdl") + "StringSliceTarget"
	}
	if anon {
		switch t.Kind() {
		case vdl.Bool, vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64, vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64, vdl.Float32, vdl.Float64, vdl.String, vdl.TypeObject:
			return data.Pkg("v.io/v23/vdl") + kindVarName(t.Kind()) + "Target"
		}
	}
	if def := data.Env.FindTypeDef(t); def != nil {
		pkg := def.File.Package
		if pkg != data.Package {
			return data.Pkg(pkg.GenPath) + def.Name + "Target"
		}
	}
	return ""
}

func inlineTargetField(data *goData, t, parentType *vdl.Type, name string) string {
	if t.Kind() == vdl.Any || data.Package.Path == "vdltool" {
		return ""
	}
	if isRecursiveReference(parentType, t) {
		return ""
	}
	return fmt.Sprintf(`%s %s`, name, targetTypeName(data, t))
}

// typeHasReference returns true if there is a reference in parentType
// to childType.
func isRecursiveReference(parentType, childType *vdl.Type) bool {
	return !childType.Walk(vdl.WalkAll, func(t *vdl.Type) bool {
		if t == parentType {
			return false
		}
		return true
	})
}

// createTargetCall returns a go r-value that will return two parameters, target and error.
func createTargetCall(data *goData, t, parentType *vdl.Type, targetName, input string) (call, body string) {
	// TODO(bprosnitz) For map[string]string, we can do the following:
	// val := make(map[string]string)
	// t.Value.Attributes = Attributes(val)
	// return nil, &TargetMapStringString{&val}, nil
	// Where TargetMapStringString is a standard target. Consider doing so in the future.
	if t.Kind() == vdl.Any || data.Package.Path == "vdltool" {
		return fmt.Sprintf("target, err := %sReflectTarget(%sValueOf(%s))", data.Pkg("v.io/v23/vdl"), data.Pkg("reflect"), input), ""
	}
	ref, body := genTargetRef(data, t)
	if isRecursiveReference(parentType, t) {
		return fmt.Sprintf("target, err := &%s{Value:%s}, error(nil)", ref, input), body
	}
	return fmt.Sprintf(`%[1]s.Value = %[2]s
	target, err := &%[1]s, error(nil)`, targetName, input), body
}

// genIncompatibleTypeCheck generates code that will test for compatibility with the provided type.
// numOutArgs is the number of additional out args that the generated function should return.
func genIncompatibleTypeCheck(data *goData, t *vdl.Type, numOutArgs int) string {
	return fmt.Sprintf(`if ttWant := %[1]s; !%[2]sCompatible(tt, ttWant) {
		return %[3]s%[4]sErrorf("type %%v incompatible with %%v", tt, ttWant)
	}`, typedConst(data, vdl.TypeObjectValue(t)), data.Pkg("v.io/v23/vdl"), strings.Repeat("nil, ", numOutArgs), data.Pkg("fmt"))
}

// targetBaseRef generates a reference to a TargetBase to embed (or
// the Target itself if there is a cyclic dependency issue with TargetBase)
func targetBaseRef(data *goData, targetType string) string {
	return fmt.Sprintf("%s%sBase", data.Pkg("v.io/v23/vdl"), targetType)
}

// genResetValue generates code that will reset the provided variable of the provided type.
func genResetValue(data *goData, t *vdl.Type, varName string) string {
	if containsNativeType(data, t) {
		return fmt.Sprintf(`%[1]s = %[3]sZero(%[3]sTypeOf(%[1]s)).Interface().(%[2]s)`, varName, typeGo(data, t), data.Pkg("reflect"))
	} else {
		return fmt.Sprintf(`%s = %s`, varName, typedConst(data, vdl.ZeroValue(t)))
	}
}

// genValueField generates either
// "Value *X" or
// "Value *NativeX
// wireValue *WireX" along with a name to reference the wireValue
func genValueField(data *goData, t *vdl.Type) (assignmentName, regName, def string) {
	if isNativeType(data.Env, t) {
		return "t.wireValue", "t.wireValue", fmt.Sprintf(`Value *%s
	wireValue %s`, typeGo(data, t), typeGoWire(data, t))
	} else {
		return "*t.Value", "t.Value", fmt.Sprintf(`Value *%s`, typeGo(data, t))
	}
}

// genResetWire generates code that will reset the wireValue if applicable
func genResetWire(data *goData, t *vdl.Type) string {
	if isNativeType(data.Env, t) {
		return fmt.Sprintf(`t.wireValue = %s`, typedConstWire(data, vdl.ZeroValue(t)))
	}
	return ""
}

// genWireToNativeConversion generates a wiretype to native conversion, or empty string
// if the provided type is not a native type.
func genWireToNativeConversion(data *goData, t *vdl.Type) string {
	if isNativeType(data.Env, t) {
		wirePkgName, name := wiretypeLocalName(data, t)
		wireType := wirePkgName + name
		return fmt.Sprintf(`
	if err := %s%sToNative(%s, %s); err != nil {
		return err
	}`, wirePkgName, wireType, "t.wireValue", "t.Value")
	}
	return ""
}

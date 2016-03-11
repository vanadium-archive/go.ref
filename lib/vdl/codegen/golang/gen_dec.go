// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"encoding/hex"
	"fmt"
	"strings"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/vdlutil"
)

// genBasicTargetDef generate's a Target definition that involves simple
// assignment.
func genBasicTargetDef(data goData, t *vdl.Type, X, fromType string) string {
	return fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[5]s
}
func (t *%[1]s) From%[6]s(src %[7]s, tt *%[4]sType) error {
	%[3]s
	*t.Value = %[2]s(src)
	return nil
}`, targetTypeName(data, t), typeGo(data, t), genIncompatibleTypeCheck(data, t, 0), data.Pkg("v.io/v23/vdl"), targetBaseRef(data, "Target"), X, fromType)
}

// genNumberTargetDef generates a Target that assigns to a number and
// performs numeric conversions.
func genNumberTargetDef(data goData, t *vdl.Type) string {
	s := fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[3]s
}`, targetTypeName(data, t), typeGo(data, t), targetBaseRef(data, "Target"))
	s += genNumberFromX(data, t, vdl.Uint64)
	s += genNumberFromX(data, t, vdl.Int64)
	s += genNumberFromX(data, t, vdl.Float64)
	s += genNumberFromX(data, t, vdl.Complex128)
	return s
}

// genNumberFromX generates a FromX method, e.g. FromInt()
func genNumberFromX(data goData, targetType *vdl.Type, sourceKind vdl.Kind) string {
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
	case vdl.Complex128:
		X = "Complex"
		fromType = "complex128"
	default:
		panic("invalid source kind")
	}
	return fmt.Sprintf(`
	func (t *%s) From%s(src %s, tt *%sType) error {
		%s
		return nil
	}`, targetTypeName(data, targetType), X, fromType, data.Pkg("v.io/v23/vdl"), genNumberConversion(data, targetType, sourceKind))
}

// genNumberConversion generates the code lines needed to perform number conversion.
func genNumberConversion(data goData, targetType *vdl.Type, sourceKind vdl.Kind) string {
	targetKind := targetType.Kind()
	targetKindName := vdlutil.FirstRuneToUpper(targetKind.String())
	if targetKindName == "Byte" {
		targetKindName = "Uint8"
	}
	sourceKindName := vdlutil.FirstRuneToUpper(sourceKind.String())
	targetTypeName := typeGo(data, targetType)

	if targetKind == sourceKind {
		return fmt.Sprintf(`*t.Value = %s(src)`, targetTypeName)
	} else {
		return fmt.Sprintf(`val, err := %s%sTo%s(src)
	if err != nil {
		return err
	}
	*t.Value = %s(val)`, data.Pkg("v.io/v23/vdl/vdlconv"), sourceKindName, targetKindName, targetTypeName)
	}
}

// genBytesDef generates a Target for a named []byte type.
func genBytesDef(data goData, t *vdl.Type) string {
	if t.Kind() == vdl.List {
		return fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[5]s
}
func (t *%[1]s) FromBytes(src []byte, tt *%[4]sType) error {
	%[3]s
	if len(src) == 0 {
		*t.Value = nil
	} else {
		*t.Value = make([]byte, len(src))
		copy(*t.Value, src)
	}
	return nil
}`, targetTypeName(data, t), typeGo(data, t), genIncompatibleTypeCheck(data, t, 0), data.Pkg("v.io/v23/vdl"), targetBaseRef(data, "Target"))
	} else {
		return fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[5]s
}
func (t *%[1]s) FromBytes(src []byte, tt *%[4]sType) error {
	%[3]s
	copy((*t.Value)[:], src)
	return nil
}`, targetTypeName(data, t), typeGo(data, t), genIncompatibleTypeCheck(data, t, 0), data.Pkg("v.io/v23/vdl"), targetBaseRef(data, "Target"))
	}
}

// genStructTargetDef generates a Target that assigns to a struct
func genStructTargetDef(data goData, t *vdl.Type) string {
	origType := t
	if t.Kind() == vdl.Optional {
		t = t.Elem()
	}
	if t.Name() == "" {
		panic("only named structs supported in generator")
	}
	var additionalBodies string
	s := fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[3]s
	%[4]s
}
func (t *%[1]s) StartFields(tt *%[5]sType) (%[5]sFieldsTarget, error) {
	%[6]s
	return t, nil
}
func (t *%[1]s) StartField(name string) (key, field %[5]sTarget, _ error) {
	switch name {`, targetTypeName(data, t), typeGo(data, t), targetBaseRef(data, "Target"), targetBaseRef(data, "FieldsTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1))
	for i := 0; i < t.NumField(); i++ {
		fld := t.Field(i)
		call, body := createTargetCall(data, fld.Type, fmt.Sprintf("&t.Value.%s", fld.Name))
		additionalBodies += body
		s += fmt.Sprintf(`
	case %q:
		val, err := %s
		return nil, val, err`, fld.Name, call)
	}
	s += fmt.Sprintf(`
	default:
		return nil, nil, %sErrorf("field %%s not in struct %%v", name, %s)
	}
}`, data.Pkg("fmt"), data.typeDepends.Add(data, t))
	s += fmt.Sprintf(`
func (t *%[1]s) FinishField(_, _ %[2]sTarget) error {
	return nil
}
func (t *%[1]s) FinishFields(_ %[2]sFieldsTarget) error {
	return nil
}`, targetTypeName(data, t), data.Pkg("v.io/v23/vdl"))
	if origType.Kind() == vdl.Optional {
		s += fmt.Sprintf(`
func (t *%s) FromNil(tt *%[2]sType) error {
	*t.Value = nil
}`, targetTypeName(data, t), data.Pkg("v.io/v23/vdl"))
	}
	s += additionalBodies
	return s
}

// genOptionalStructTargetDef generates a Target that assigns to an optional
// struct - that is either calls FromNil or StartFields.
func genOptionalStructTargetDef(data goData, t *vdl.Type) string {
	call, body := createTargetCall(data, t.Elem(), "*t.Value")
	s := fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[3]s
	%[4]s
}
func (t *%[1]s) StartFields(tt *%[5]sType) (%[5]sFieldsTarget, error) {
	if *t.Value == nil {
		*t.Value = &%[7]s
	}
	target, err := %[6]s
	if err != nil {
		return nil, err
	}
	return target.StartFields(tt)
}
func (t *%[1]s) FinishFields(_ %[5]sFieldsTarget) error {
	return nil
}
func (t *%[1]s) FromNil(tt *vdl.Type) error {
	*t.Value = nil
	return nil
}`, targetTypeName(data, t), typeGo(data, t), targetBaseRef(data, "Target"), targetBaseRef(data, "FieldsTarget"), data.Pkg("v.io/v23/vdl"), call, typedConst(data, vdl.ZeroValue(t.Elem())))
	s += body
	return s
}

// genListTargetDef generates a Target that assigns to a list.
func genListTargetDef(data goData, t *vdl.Type) string {
	s := fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[3]s
	%[4]s
}
func (t *%[1]s) StartList(tt *%[5]sType, len int) (%[5]sListTarget, error) {
	%[6]s`, targetTypeName(data, t), typeGo(data, t), targetBaseRef(data, "Target"), targetBaseRef(data, "ListTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1))
	if t.Kind() == vdl.List {
		s += fmt.Sprintf(`
	if cap(*t.Value) < len {
		*t.Value = make(%s, len)
	} else {
		*t.Value = (*t.Value)[:len]
	}`, typeGo(data, t))
	}
	call, body := createTargetCall(data, t.Elem(), "&(*t.Value)[index]")
	s += fmt.Sprintf(`
	return t, nil
}
func (t *%[1]s) StartElem(index int) (elem %[2]sTarget, _ error) {
	return %[3]s
}
func (t *%[1]s) FinishElem(elem %[2]sTarget) error {
	return nil
}
func (t *%[1]s) FinishList(elem %[2]sListTarget) error {
	return nil
}`, targetTypeName(data, t), data.Pkg("v.io/v23/vdl"), call)
	s += body
	return s
}

// genSetTargetDef generates a Target that assigns to a set.
func genSetTargetDef(data goData, t *vdl.Type) string {
	call, body := createTargetCall(data, t.Key(), "&t.currKey")
	s := fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	currKey %[3]s
	%[4]s
	%[5]s
}
func (t *%[1]s) StartSet(tt *%[6]sType, len int) (%[6]sSetTarget, error) {
	%[7]s
	*t.Value = make(%[2]s)
	return t, nil
}
func (t *%[1]s) StartKey() (key %[6]sTarget, _ error) {
	%[9]s
	return %[8]s
}
func (t *%[1]s) FinishKey(key %[6]sTarget) error {
	(*t.Value)[t.currKey] = struct{}{}
	return nil
}
func (t *%[1]s) FinishSet(list %[6]sSetTarget) error {
	if len(*t.Value) == 0 {
		*t.Value = nil
	}
	return nil
}`, targetTypeName(data, t), typeGo(data, t), typeGo(data, t.Key()), targetBaseRef(data, "Target"), targetBaseRef(data, "SetTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1), call, genResetValue(data, t.Key(), "t.currKey"))
	s += body
	return s
}

// genMapTargetDef generates a Target that assigns to a map.
func genMapTargetDef(data goData, t *vdl.Type) string {
	keyCall, keyBody := createTargetCall(data, t.Key(), "&t.currKey")
	elemCall, elemBody := createTargetCall(data, t.Elem(), "&t.currElem")
	s := fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	currKey %[3]s
	currElem %[4]s
	%[5]s
	%[6]s
}
func (t *%[1]s) StartMap(tt *%[7]sType, len int) (%[7]sMapTarget, error) {
	%[8]s
	*t.Value = make(%[2]s)
	return t, nil
}
func (t *%[1]s) StartKey() (key %[7]sTarget, _ error) {
	%[11]s
	return %[9]s
}
func (t *%[1]s) FinishKeyStartField(key %[7]sTarget) (field %[7]sTarget, _ error) {
	%[12]s
	return %[10]s
}
func (t *%[1]s) FinishField(key, field %[7]sTarget) error {
	(*t.Value)[t.currKey] = t.currElem
	return nil
}
func (t *%[1]s) FinishMap(elem %[7]sMapTarget) error {
	if len(*t.Value) == 0 {
		*t.Value = nil
	}
	return nil
}`, targetTypeName(data, t), typeGo(data, t), typeGo(data, t.Key()), typeGo(data, t.Elem()), targetBaseRef(data, "Target"), targetBaseRef(data, "MapTarget"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 1), keyCall, elemCall, genResetValue(data, t.Key(), "t.currKey"), genResetValue(data, t.Elem(), "t.currElem"))
	s += keyBody
	s += elemBody
	return s
}

// genEnumTargetDef generates a Target that assigns to an enum,
func genEnumTargetDef(data goData, t *vdl.Type) string {
	s := fmt.Sprintf(`
type %[1]s struct {
	Value *%[2]s
	%[3]s
}
func (t *%[1]s) FromEnumLabel(src string, tt *%[4]sType) error {
	%[5]s
	switch src {`, targetTypeName(data, t), typeGo(data, t), targetBaseRef(data, "Target"), data.Pkg("v.io/v23/vdl"), genIncompatibleTypeCheck(data, t, 0))
	for i := 0; i < t.NumEnumLabel(); i++ {
		s += fmt.Sprintf(`
	case %q:
		*t.Value = %d`, t.EnumLabel(i), i)
	}
	s += fmt.Sprintf(`
	default:
		return %sErrorf("label %%s not in enum %%v", src, %s)
	}
	return nil
}`, data.Pkg("fmt"), data.typeDepends.Add(data, t))
	return s
}

// genTargetDef calls the appropriate Target generator based on type t.
func genTargetDef(data goData, t *vdl.Type) string {
	if isNativeType(t, data.File.Package) {
		return ""
	}
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
		case vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64, vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64, vdl.Float32, vdl.Float64, vdl.Complex64, vdl.Complex128:
			return genNumberTargetDef(data, t)
		}
	}
	return ""
}

// genTargetRef returns a string representing a the name of a Target for the provided type.
// If the Target body must be generated, it is returned as the second parameter.
func genTargetRef(data goData, t *vdl.Type) (targTypeName, body string) {
	if t == vdl.ErrorType {
		return data.Pkg("v.io/v23/verror") + "ErrorTarget", ""
	}
	if t.Name() == "" {
		if t.IsBytes() && t.Kind() == vdl.List {
			return data.Pkg("v.io/v23/vdl") + "BytesTarget", ""
		}
		if t.Kind() == vdl.List && t.Elem() == vdl.StringType {
			return data.Pkg("v.io/v23/vdl") + "StringSliceTarget", ""
		}
		switch t.Kind() {
		case vdl.Bool, vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64, vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64, vdl.Float32, vdl.Float64, vdl.Complex64, vdl.Complex128, vdl.String, vdl.TypeObject:
			return data.Pkg("v.io/v23/vdl") + kindVarName(t.Kind()) + "Target", ""
		}
	}

	if def := data.Env.FindTypeDef(t); (def != nil && def.File == data.File) || t.Name() == "" {
		body = genTargetDef(data, t)
	}
	targTypeName = targetTypeName(data, t)
	return
}

// targetTypeName generates a stable name for a Target for the specified type.
func targetTypeName(data goData, t *vdl.Type) string {
	if t.Name() != "" {
		pkgPath, name := vdl.SplitIdent(t.Name())
		if !strings.ContainsRune(pkgPath, '/') {
			pkgPath = "v.io/v23/vdlroot/" + pkgPath
		}
		return data.Pkg(pkgPath) + name + "Target"
	} else {
		return strings.TrimSuffix(data.File.BaseName, ".vdl") + hex.EncodeToString([]byte(t.String())) + "Target"
	}
}

// createTargetCall returns a go r-value that will return two parameters, target and error.
func createTargetCall(data goData, t *vdl.Type, input string) (call, body string) {
	// TODO(bprosnitz) For map[string]string, we can do the following:
	// val := make(map[string]string)
	// t.Value.Attributes = Attributes(val)
	// return nil, &TargetMapStringString{&val}, nil
	// Where TargetMapStringString is a standard target. Consider doing so in the future.
	if t.Kind() == vdl.Union || t.Kind() == vdl.Any || isNativeType(t, data.File.Package) {
		return fmt.Sprintf("%sReflectTarget(%sValueOf(%s))", data.Pkg("v.io/v23/vdl"), data.Pkg("reflect"), input), ""
	}
	ref, body := genTargetRef(data, t)
	return fmt.Sprintf("&%s{Value:%s}, error(nil)", ref, input), body
}

// genIncompatibleTypeCheck generates code that will test for compatibility with the provided type.
// numOutArgs is the number of additional out args that the generated function should return.
func genIncompatibleTypeCheck(data goData, t *vdl.Type, numOutArgs int) string {
	return fmt.Sprintf(`if !%[1]sCompatible(tt, %[2]s) {
		return %[3]s%[4]sErrorf("type %%v incompatible with %%v", tt, %[2]s)
	}`, data.Pkg("v.io/v23/vdl"), data.typeDepends.Add(data, t), strings.Repeat("nil, ", numOutArgs), data.Pkg("fmt"))
}

// targetBaseRef generates a reference to a TargetBase to embed (or
// the Target itself if there is a cyclic dependency issue with TargetBase)
func targetBaseRef(data goData, targetType string) string {
	return fmt.Sprintf("%s%sBase", data.Pkg("v.io/v23/vdl"), targetType)
}

// genResetValue generates code that will reset the provided variable of the provided type.
func genResetValue(data goData, t *vdl.Type, varName string) string {
	if containsNativeType(data, t) {
		return fmt.Sprintf(`%[1]s = reflect.Zero(reflect.TypeOf(%[1]s)).Interface().(%[2]s)`, varName, typeGo(data, t))
	} else {
		return fmt.Sprintf(`%s = %s`, varName, typedConst(data, vdl.ZeroValue(t)))
	}
}

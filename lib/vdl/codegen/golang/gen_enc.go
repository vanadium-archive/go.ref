// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"
	"sort"
	"strings"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
	"v.io/x/ref/lib/vdl/vdlutil"
)

// genEncDef generates the body of an encoder definition for the the provided type
//
// unionFieldName is the name of the union field (corresponding to the struct
// that the encode method will be on) if the type is a union
func genEncDef(data goData, t *vdl.Type, unionFieldName string) string {
	varCount := 0
	instName := "(*m)"
	if t.Kind() == vdl.Struct || t.Kind() == vdl.Union {
		// - Struct shouldn't be dereferenced because its value is passed around
		// as a pointer.
		// - Unions shouldn't be dereferenced because the structs representing
		// their fields are not represented as pointers within the union interface.
		// e.g. Union A (an interface) contains struct AB: A(AB{}) rather than
		// A(&AB{})
		instName = "m"
	}
	return genEncDefInternal(data, t, instName, "t", unionFieldName, "tt", &varCount)
}

func genEncDefInternal(data goData, t *vdl.Type, instName, targetName, unionFieldName, forcedTypeName string, varCount *int) string {
	if prim := genFromPrimitive(data, t, instName, targetName, varCount); prim != "" {
		return prim
	}

	switch t.Kind() {
	case vdl.TypeObject:
		typeObjectValName := createUniqueName("typeObjectVal", varCount)
		return fmt.Sprintf(`%[3]s := %[2]s
		if %[3]s == nil {
			%[3]s = %[4]sAnyType
		}
		if err := %[1]s.FromTypeObject(%[3]s); err != nil {
			return err
		}`, targetName, instName, typeObjectValName, data.Pkg("v.io/v23/vdl"))

	case vdl.Any:
		if shouldUseVdlValueForAny(data.Package) {
			return fmt.Sprintf(`
		if %[2]s == nil {
			if err := %[1]s.FromNil(%[4]s); err != nil {
				return err
			}
		} else {
			if err := %[3]sFromValue(%[1]s, %[2]s); err != nil {
				return err
			}
		}`, targetName, instName, data.Pkg("v.io/v23/vdl"), data.typeDepends.Add(data, t))
		} else {
			return fmt.Sprintf(`
		if %[2]s == nil {
			if err := %[1]s.FromNil(%[3]s); err != nil {
				return err
			}
		} else {
			if err := %[2]s.FillVDLTarget(%[1]s, %[3]s); err != nil {
				return err
			}
		}`, targetName, instName, data.typeDepends.Add(data, t))
		}

	case vdl.Array, vdl.List:
		if t.Elem().Kind() == vdl.Byte {
			var arrayModifier string
			if t.Kind() == vdl.Array {
				arrayModifier = "[:]"
			}
			return fmt.Sprintf(`
	if err := %s.FromBytes([]byte(%s%s), %s); err != nil {
		return err
	}`, targetName, instName, arrayModifier, data.typeDepends.Add(data, t))
		}
		listTargetName := createUniqueName("listTarget", varCount)
		var s string
		if t.Kind() == vdl.Array {
			s += fmt.Sprintf(`
	%s, err := %s.StartList(%s, %d)`, listTargetName, targetName, data.typeDepends.Add(data, t), t.Len())
		} else {
			s += fmt.Sprintf(`
	%s, err := %s.StartList(%s, len(%s))`, listTargetName, targetName, data.typeDepends.Add(data, t), instName)
		}
		elemTargetName := createUniqueName("elemTarget", varCount)
		elemName := createUniqueName("elem", varCount)
		s += fmt.Sprintf(`
	if err != nil {
		return err
	}
	for i, %s := range %s {
		%s, err := %s.StartElem(i)
		if err != nil {
			return err
		}
		%s
		if err := %s.FinishElem(%s); err != nil {
			return err
		}
	}
	if err := %s.FinishList(%s); err != nil {
		return err
	}`, elemName, instName, elemTargetName, listTargetName,
			genEncRef(data, t.Elem(), elemName, elemTargetName, varCount),
			listTargetName, elemTargetName, targetName, listTargetName)
		return s

	case vdl.Set:
		setTargetName := createUniqueName("setTarget", varCount)
		s := fmt.Sprintf(`
	%s, err := %s.StartSet(%s, len(%s))`, setTargetName, targetName, data.typeDepends.Add(data, t), instName)
		keyTargetName := createUniqueName("keyTarget", varCount)
		keyName := createUniqueName("key", varCount)
		s += fmt.Sprintf(`
	if err != nil {
		return err
	}
	for %s := range %s {
		%s, err := %s.StartKey()
		if err != nil {
			return err
		}
		%s
		if err := %s.FinishKey(%s); err != nil {
			return err
		}
	}
	if err := %s.FinishSet(%s); err != nil {
		return err
	}`, keyName, instName, keyTargetName, setTargetName,
			genEncRef(data, t.Key(), keyName, keyTargetName, varCount),
			setTargetName, keyTargetName, targetName, setTargetName)
		return s

	case vdl.Map:
		mapTargetName := createUniqueName("mapTarget", varCount)
		s := fmt.Sprintf(`
	%s, err := %s.StartMap(%s, len(%s))`, mapTargetName, targetName, data.typeDepends.Add(data, t), instName)
		keyTargetName := createUniqueName("keyTarget", varCount)
		keyName := createUniqueName("key", varCount)
		valueTargetName := createUniqueName("valueTarget", varCount)
		valueName := createUniqueName("value", varCount)
		s += fmt.Sprintf(`
	if err != nil {
		return err
	}
	for %s, %s := range %s {
		%s, err := %s.StartKey()
		if err != nil {
			return err
		}
		%s
		%s, err := %s.FinishKeyStartField(%s)
		if err != nil {
			return err
		}
		%s
		if err := %s.FinishField(%s, %s); err != nil {
			return err
		}
	}
	if err := %s.FinishMap(%s); err != nil {
		return err
	}`, keyName, valueName, instName, keyTargetName, mapTargetName,
			genEncRef(data, t.Key(), keyName, keyTargetName, varCount),
			valueTargetName, mapTargetName, keyTargetName,
			genEncRef(data, t.Elem(), valueName, valueTargetName, varCount),
			mapTargetName, keyTargetName, valueTargetName, targetName, mapTargetName)
		return s

	case vdl.Struct:
		fieldsTargetName := createUniqueName("fieldsTarget", varCount)
		name := forcedTypeName
		var s string
		if name == "" {
			// inline
			name = data.typeDepends.Add(data, t)
		} else {
			// not inline / top of function def - check that we have the right type
			if containsNativeType(data, t) {
				// Because native types are initialized in init() blocks, we need
				// another way to initialize their values if other init blocks
				// call FillVDLTarget().
				s += fmt.Sprintf("\n__VDLEnsureNativeBuilt()")
			} else {
				// TODO(bprosnitz) This only checks if two types are nil. Check if all used types are nil.
				s += fmt.Sprintf(`
	if %s == nil || %s == nil {
		panic("Initialization order error: types generated for FillVDLTarget not initialized. Consider moving caller to an init() block.")
	}`, data.typeDepends.Add(data, t), data.typeDepends.Add(data, vdl.OptionalType(t)))
			}
			fmt.Sprintf(`if %[1]s != %[2]s && %[1]s != %[3]s {
		panic("FillVDLTarget called with invalid type")
	}`, forcedTypeName, data.typeDepends.Add(data, t), data.typeDepends.Add(data, vdl.OptionalType(t)))
		}
		s += fmt.Sprintf(`
	%[1]s, err := %[2]s.StartFields(%[3]s)
	if err != nil {
	return err
	}
	`, fieldsTargetName, targetName, name)
		for ix := 0; ix < t.NumField(); ix++ {
			f := t.Field(ix)
			fieldInstName := fmt.Sprintf("%s.%s", instName, f.Name)
			fieldInstName, nativeConvBody := encWiretypeInstName(data, f.Type, fieldInstName, varCount)
			s += nativeConvBody
			// TODO(toddw): Add native IsZero check here?
			keyTargetName := createUniqueName("keyTarget", varCount)
			fieldTargetName := createUniqueName("fieldTarget", varCount)
			s += fmt.Sprintf(`
		%[1]s, %[2]s, err := %[3]s.StartField(%[4]q)
		if err != %[5]sErrFieldNoExist && err != nil {
			return err
		}
		if err != %[5]sErrFieldNoExist {
			%[6]s
			if err := %[7]s.FinishField(%[8]s, %[9]s); err != nil {
				return err
			}
		}`, keyTargetName, fieldTargetName, fieldsTargetName, f.Name, data.Pkg("v.io/v23/vdl"),
				genEncRefForWiretype(data, f.Type, fieldInstName, fieldTargetName, varCount),
				fieldsTargetName, keyTargetName, fieldTargetName)
		}
		s += fmt.Sprintf(`
	if err := %s.FinishFields(%s); err != nil {
		return err
	}`, targetName, fieldsTargetName)
		return s

	case vdl.Union:
		fieldsTargetName := createUniqueName("fieldsTarget", varCount)
		keyTargetName := createUniqueName("keyTarget", varCount)
		fieldTargetName := createUniqueName("fieldTarget", varCount)
		unionField, _ := t.FieldByName(unionFieldName)
		fieldInstName := fmt.Sprintf("%s.Value", instName)
		return fmt.Sprintf(`
	%s, err := %s.StartFields(%s)
	if err != nil {
		return err
	}
	%s, %s, err := %s.StartField(%q)
	if err != nil {
		return err
	}
	%s
	if err := %s.FinishField(%s, %s); err != nil {
		return err
	}
	if err := %s.FinishFields(%s); err != nil {
		return err
	}
	`, fieldsTargetName, targetName, data.typeDepends.Add(data, t),
			keyTargetName, fieldTargetName, fieldsTargetName, unionFieldName,
			genEncRef(data, unionField.Type, fieldInstName, fieldTargetName, varCount),
			fieldsTargetName, keyTargetName, fieldTargetName,
			targetName, fieldsTargetName)
	}
	panic(fmt.Sprintf("encoder for kind %v unimplemented", t.Kind()))
}

var wireErrorType = vdl.TypeOf(vdl.WireError{})

// genEncRef generates either a reference to a named encoder or the body of an
// encoder that must be inlined (e.g. for a unnamed list)
func genEncRef(data goData, t *vdl.Type, instName, targetName string, varCount *int) string {
	wireInstName, s := encWiretypeInstName(data, t, instName, varCount)
	s += genEncRefForWiretype(data, t, wireInstName, targetName, varCount)
	return s
}

// genEncRefForWiretype is the same as genRef, but it assumes that the instance name refers
// to a wiretype value.
func genEncRefForWiretype(data goData, t *vdl.Type, wireInstName, targetName string, varCount *int) string {
	origType := t
	var s string

	// If this is an optional, add the nil case.
	if origType.Kind() == vdl.Optional {
		s += fmt.Sprintf(`
	if %s == nil {
		if err := %s.FromNil(%s); err != nil {
			return err
		}
	} else {`, wireInstName, targetName, data.typeDepends.Add(data, t))
		t = t.Elem()
	}

	// If this is an error, add a convert to the corresponding wire error.
	if t == wireErrorType {
		wireErrorName := createUniqueName("wireError", varCount)
		s += fmt.Sprintf(`
	var %[1]s %[3]sWireError
	if err := %[2]sWireFromNative(&%[1]s, %[4]s); err != nil {
		return err
	}
	if err := %[1]s.FillVDLTarget(%[5]s, %[3]sErrorType); err != nil {
		return err
	}
	`, wireErrorName, data.Pkg("v.io/v23/verror"), data.Pkg("v.io/v23/vdl"), wireInstName, targetName)
		if origType.Kind() == vdl.Optional {
			s += `
	}`
		}
		return s
	}

	// If this is a union value, give it the default value if it is nil.
	if t.Kind() == vdl.Union {
		pkgPath, name := vdl.SplitIdent(t.Name())
		unionName := createUniqueName("unionValue", varCount)
		s += fmt.Sprintf(`
	%[1]s := %[2]s
	if %[1]s == nil {
		%[1]s = %[3]s{}
	}`, unionName, wireInstName, data.Pkg(pkgPath)+name+t.Field(0).Name)
		wireInstName = unionName
	}

	if t.Name() != "" {
		// Call the inner encoder
		s += fmt.Sprintf(`
	if err := %s.FillVDLTarget(%s, %s); err != nil {
		return err
	}`, wireInstName, targetName, data.typeDepends.Add(data, t))
	} else {
		// Generate an inline encoder
		s += genEncDefInternal(data, t, wireInstName, targetName, "", "", varCount)
	}

	if origType.Kind() == vdl.Optional {
		s += `
	}`
	}

	return s
}

// encWiretypeInstName ensures that inst name is a wiretype. It is a no-op for non-native types
// but generates conversion code for native types.
func encWiretypeInstName(data goData, t *vdl.Type, instName string, varCount *int) (wiretypeInstName, body string) {
	// If this is a native type, convert it to wire type.
	if isNativeType(t, data.Package) {
		wirePkgName, name := wiretypeLocalName(data, t)
		wireType := wirePkgName + name
		wiretypeInstName = createUniqueName("wireValue", varCount)
		body = fmt.Sprintf(`
	var %s %s
	if err := %s%sFromNative(&%s, %s); err != nil {
		return err
	}
	`, wiretypeInstName, wireType, wirePkgName, name, wiretypeInstName, instName)
		instName = wiretypeInstName
		return
	}

	// If this isn't native, just return the original instName.
	return instName, ""
}

func wiretypeLocalName(data goData, t *vdl.Type) (pkgName, name string) {
	pkgPath, name := vdl.SplitIdent(t.Name())
	return data.Pkg(data.Env.ResolvePackage(pkgPath).GenPath), name
}

// genFromPrimitive generates a fromX() call corresponding with the appropriate primitive type.
func genFromPrimitive(data goData, t *vdl.Type, instName, targetName string, varCount *int) string {
	var methodName string
	var castType string
	switch t.Kind() {
	case vdl.Bool:
		methodName = "FromBool"
		castType = "bool"
	case vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64:
		methodName = "FromUint"
		castType = "uint64"
	case vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64:
		methodName = "FromInt"
		castType = "int64"
	case vdl.Float32, vdl.Float64:
		methodName = "FromFloat"
		castType = "float64"
	case vdl.Complex64, vdl.Complex128:
		methodName = "FromComplex"
		castType = "complex128"
	case vdl.String:
		methodName = "FromString"
		castType = "string"
	case vdl.Enum:
		return fmt.Sprintf(`if err := %s.FromEnumLabel(%s.String(), %s); err != nil {
	return err
	}`, targetName, instName, data.typeDepends.Add(data, t))
	case vdl.Array, vdl.List:
		if t.IsBytes() {
			methodName = "FromBytes"
			castType = "[]byte"
		}
		return ""
	default:
		return ""
	}
	var typeName string
	if t.Name() != "" {
		typeName = data.typeDepends.Add(data, t)
	} else {
		typeName = data.Pkg("v.io/v23/vdl") + kindVarName(t.Kind()) + "Type"
	}
	return fmt.Sprintf(`if err := %s.%s(%s(%s), %s); err != nil {
	return err
}`, targetName, methodName, castType, instName, typeName)
}

// kindVarName returns the go name of the kind variable in the vdl package
// e.g. vdl.Int -> "Int"
// This is useful because kind.String() returns a string with a different casing rule.
func kindVarName(k vdl.Kind) string {
	if k == vdl.TypeObject {
		return "TypeObject"
	}
	return vdlutil.FirstRuneToUpper(k.String())
}

// genVdlTypeBuilder generates code to build a type using vdl.TypeBuilder.
//
// It is necessary (currently) because typedConst doesn't work with native types and
// it is not possible to call vdl.TypeOf(*X) for arbitrary X because of the rules
// on what can be optional.
// TODO(bprosnitz) Consider removing this
func genVdlTypeBuilder(data goData, t *vdl.Type, varName string, varCount *int) string {
	lookupName := map[*vdl.Type]string{}
	builderBody := genVdlTypeBuilderInternal(data, t, lookupName, varName, varCount)
	return fmt.Sprintf(`
	%[1]sBuilder := %[2]sTypeBuilder{}
	%[3]s
	%[1]sBuilder.Build()
	%[1]sv, err := %[4]s.Built()
	if err != nil {
		panic(err)
	}
	return %[1]sv`, varName, data.Pkg("v.io/v23/vdl"), builderBody, lookupName[t])
}

func genVdlTypeBuilderInternal(data goData, t *vdl.Type, lookupName map[*vdl.Type]string, prefix string, varcount *int) (str string) {
	if _, ok := lookupName[t]; ok {
		return ""
	}
	unnamedName := createUniqueName(prefix, varcount)
	switch t.Kind() {
	case vdl.List, vdl.Array, vdl.Optional, vdl.Set, vdl.Map, vdl.Struct, vdl.Union, vdl.Enum:
		str += fmt.Sprintf(`
	%s := %sBuilder.%s()`, unnamedName, prefix, kindVarName(t.Kind()))
	default:
		str += fmt.Sprintf(`
	%s := %s%sType`, unnamedName, data.Pkg("v.io/v23/vdl"), kindVarName(t.Kind()))
	}
	var name string
	if t.Name() != "" {
		name = createUniqueName(prefix, varcount)
		str += fmt.Sprintf(`
	%s := %sBuilder.Named(%q).AssignBase(%s)`, name, prefix, t.Name(), unnamedName)
	} else {
		name = unnamedName
	}
	lookupName[t] = name

	switch t.Kind() {
	case vdl.List, vdl.Array, vdl.Optional:
		str += genVdlTypeBuilderInternal(data, t.Elem(), lookupName, prefix, varcount)
		str += fmt.Sprintf(`
	%s.AssignElem(%s)`, unnamedName, lookupName[t.Elem()])
		if t.Kind() == vdl.Array {
			str += fmt.Sprintf(`
	%s.AssignLen(%d)`, unnamedName, t.Len())
		}
	case vdl.Set:
		str += genVdlTypeBuilderInternal(data, t.Key(), lookupName, prefix, varcount)
		str += fmt.Sprintf(`
	%s.AssignKey(%s)`, unnamedName, lookupName[t.Key()])
	case vdl.Map:
		str += genVdlTypeBuilderInternal(data, t.Key(), lookupName, prefix, varcount)
		str += fmt.Sprintf(`
	%s.AssignKey(%s)`, unnamedName, lookupName[t.Key()])
		str += genVdlTypeBuilderInternal(data, t.Elem(), lookupName, prefix, varcount)
		str += fmt.Sprintf(`
	%s.AssignElem(%s)`, unnamedName, lookupName[t.Elem()])
	case vdl.Struct, vdl.Union:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			str += genVdlTypeBuilderInternal(data, f.Type, lookupName, prefix, varcount)
			str += fmt.Sprintf(`
	%s.AppendField(%q, %s)`, unnamedName, f.Name, lookupName[f.Type])
		}
	case vdl.Enum:
		for i := 0; i < t.NumEnumLabel(); i++ {
			str += fmt.Sprintf(`
	%s.AppendLabel(%q)`, unnamedName, t.EnumLabel(i))
		}
	}
	return
}

func containsNativeType(data goData, t *vdl.Type) bool {
	isNative := !t.Walk(vdl.WalkAll, func(wt *vdl.Type) bool {
		return !isNativeType(wt, data.Package)
	})
	return isNative
}

// genVdlTypesForEnc generates *vdl.Type constants needed by the encoder.
// Generates strings of the form "var __a_type_name = vdl.TypeOf(X{})"
//
// NOTE: This code MUST be generated after RegisterNative() calls, or
// vdl.TypeOf() will error when Registering the wire type.
func genVdlTypesForEnc(data goData) (s string) {
	for _, t := range data.typeDepends.SortedTypes() {
		objName := data.typeDepends.types[t]
		if containsNativeType(data, t) {
			var varCount int
			s += fmt.Sprintf(`
	var %[1]s *%[2]sType
	func %[1]s_gen() *%[2]sType{`, objName, data.Pkg("v.io/v23/vdl"))
			// Generate a builder in the case that we have native types.
			// TODO This is termporary and if it must exist, it should be moved
			// to typedConst.
			// It is not possible to know how to create a zero value instance
			// of a native type directly with the currently available information,
			// so some solutions are either to use a builder or to create an instance
			// of the wire type in the init function and then convert it.
			s += genVdlTypeBuilder(data, t, objName, &varCount)
			s += fmt.Sprintf(`
	}
	func init() {
		%[1]s = %[1]s_gen()
	}`, objName)
		} else {
			s += fmt.Sprintf(`
		var %s *%sType = %s`, objName, data.Pkg("v.io/v23/vdl"), typedConst(data, vdl.TypeObjectValue(t)))
		}
	}

	s += fmt.Sprintf("\nfunc __VDLEnsureNativeBuilt() {")
	for _, t := range data.typeDepends.SortedTypes() {
		if containsNativeType(data, t) {
			s += fmt.Sprintf(`
	if %[1]s == nil {
		%[1]s = %[1]s_gen()
	}`, data.typeDepends.types[t])
		}
	}
	s += "\n}"

	return
}

// isNativeType traverses the package structure to determine whether the provided type is native.
func isNativeType(t *vdl.Type, pkg *compile.Package) bool {
	pkgPath, name := vdl.SplitIdent(t.Name())
	return isNativeTypeInternal(pkgPath, name, pkg, map[*compile.Package]bool{})
}

func isNativeTypeInternal(pkgPath, name string, pkg *compile.Package, seen map[*compile.Package]bool) bool {
	if seen[pkg] {
		return false
	}
	seen[pkg] = true

	if pkg.Path == pkgPath {
		_, ok := pkg.Config.Go.WireToNativeTypes[name]
		return ok
	}

	for _, f := range pkg.Files {
		for _, dep := range f.PackageDeps {
			if isNativeTypeInternal(pkgPath, name, dep, seen) {
				return true
			}
		}
	}
	return false
}

func createUniqueName(prefix string, varCount *int) string {
	*varCount++
	return fmt.Sprintf("%s%d", prefix, *varCount)
}

type typeDependencyNames struct {
	types     map[*vdl.Type]string
	nextCount int
}

func newTypeDependencyNames() *typeDependencyNames {
	return &typeDependencyNames{
		types: map[*vdl.Type]string{},
	}
}

var basicTypes map[*vdl.Type]string = map[*vdl.Type]string{
	vdl.AnyType:        "AnyType",
	vdl.BoolType:       "BoolType",
	vdl.ByteType:       "ByteType",
	vdl.Uint16Type:     "Uint16Type",
	vdl.Uint32Type:     "Uint32Type",
	vdl.Uint64Type:     "Uint64Type",
	vdl.Int8Type:       "Int8Type",
	vdl.Int16Type:      "Int16Type",
	vdl.Int32Type:      "Int32Type",
	vdl.Int64Type:      "Int64Type",
	vdl.Float32Type:    "Float32Type",
	vdl.Float64Type:    "Float64Type",
	vdl.Complex64Type:  "Complex64Type",
	vdl.Complex128Type: "Complex128Type",
	vdl.StringType:     "StringType",
	vdl.TypeObjectType: "TypeObjectType",
	vdl.ErrorType:      "ErrorType",
}

func (typeDepends *typeDependencyNames) Add(data goData, t *vdl.Type) string {
	if str, ok := basicTypes[t]; ok {
		return data.Pkg("v.io/v23/vdl") + str
	}
	if str, ok := typeDepends.types[t]; ok {
		return str
	}
	var str string
	if t.Name() != "" {
		str = fmt.Sprintf("__VDLType_%s", strings.Replace(strings.Replace(t.Name(), ".", "_", -1), "/", "_", -1))
	} else {
		str = fmt.Sprintf("__VDLType%d", typeDepends.nextCount)
		typeDepends.nextCount++
	}
	typeDepends.types[t] = str
	return str
}

type sortableTypes []*vdl.Type

func (st sortableTypes) Len() int {
	return len(st)
}
func (st sortableTypes) Less(i, j int) bool {
	return st[i].String() < st[j].String()
}
func (st sortableTypes) Swap(i, j int) {
	st[i], st[j] = st[j], st[i]
}

func (typeDepends *typeDependencyNames) SortedTypes() []*vdl.Type {
	types := make(sortableTypes, 0, len(typeDepends.types))
	for key := range typeDepends.types {
		types = append(types, key)
	}
	sort.Sort(types)
	return []*vdl.Type(types)
}

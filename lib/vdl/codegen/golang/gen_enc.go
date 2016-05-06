// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
	"v.io/x/ref/lib/vdl/vdlutil"
)

// genEncDef generates the body of an encoder definition for the the provided type
//
// unionFieldName is the name of the union field (corresponding to the struct
// that the encode method will be on) if the type is a union
func genEncDef(data *goData, t *vdl.Type, unionFieldName string) string {
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
	return genEncDefInternal(data, t, instName, "t", unionFieldName, "", &varCount)
}

func ttVarName(ttSuffix string) string {
	if ttSuffix == "" {
		return "tt"
	}
	return "tt.NonOptional()" + ttSuffix
}

func genEncDefInternal(data *goData, t *vdl.Type, instName, targetName, unionFieldName, ttSuffix string, varCount *int) string {
	ttVar := ttVarName(ttSuffix)
	if prim := genFromScalar(data, t, instName, targetName, ttSuffix, varCount); prim != "" {
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
		switch goAnyRepMode(data.Package) {
		case goAnyRepValue:
			return fmt.Sprintf(`
		if err := %[3]sFromValue(%[1]s, %[2]s); err != nil {
			return err
		}`, targetName, instName, data.Pkg("v.io/v23/vdl"))
		case goAnyRepRawBytes:
			return fmt.Sprintf(`
		if err := %[2]s.FillVDLTarget(%[1]s, %[3]s); err != nil {
			return err
		}`, targetName, instName, ttVar)
		default:
			// TODO(toddw): the interface{} any representation isn't supported; this
			// entire file will be removed soon anyways.
			return ""
		}

	case vdl.Array, vdl.List:
		// Check for special-cases []byte and [N]byte, which can use FromBytes.
		// Note that []X and [N]X cannot use this special-case, since Go doesn't
		// allow conversion from []X to []byte, even if the underlying type of X is
		// byte.
		if t.Elem() == vdl.ByteType {
			var arrayModifier string
			if t.Kind() == vdl.Array {
				arrayModifier = "[:]"
			}
			return fmt.Sprintf(`
	if err := %[1]s.FromBytes([]byte(%[2]s%[3]s), %[4]s); err != nil {
		return err
	}`, targetName, instName, arrayModifier, ttVar)
		}
		listTargetName := createUniqueName("listTarget", varCount)
		var s string
		if t.Kind() == vdl.Array {
			s += fmt.Sprintf(`
	%[1]s, err := %[2]s.StartList(%[3]s, %[4]d)`, listTargetName, targetName, ttVar, t.Len())
		} else {
			s += fmt.Sprintf(`
	%[1]s, err := %[2]s.StartList(%[3]s, len(%[4]s))`, listTargetName, targetName, ttVar, instName)
		}
		elemTargetName := createUniqueName("elemTarget", varCount)
		elemName := createUniqueName("elem", varCount)
		s += fmt.Sprintf(`
	if err != nil {
		return err
	}
	for i, %[1]s := range %[2]s {
		%[3]s, err := %[4]s.StartElem(i)
		if err != nil {
			return err
		}
		%[5]s
		if err := %[4]s.FinishElem(%[3]s); err != nil {
			return err
		}
	}
	if err := %[6]s.FinishList(%[4]s); err != nil {
		return err
	}`, elemName, instName, elemTargetName, listTargetName,
			genEncRef(data, t.Elem(), elemName, elemTargetName, ttSuffix+".Elem()", varCount),
			targetName)
		return s

	case vdl.Set:
		setTargetName := createUniqueName("setTarget", varCount)
		s := fmt.Sprintf(`
	%[1]s, err := %[2]s.StartSet(%[3]s, len(%[4]s))`, setTargetName, targetName, ttVar, instName)
		keyTargetName := createUniqueName("keyTarget", varCount)
		keyName := createUniqueName("key", varCount)
		s += fmt.Sprintf(`
	if err != nil {
		return err
	}
	for %[1]s := range %[2]s {
		%[3]s, err := %[4]s.StartKey()
		if err != nil {
			return err
		}
		%[5]s
		if err := %[4]s.FinishKey(%[3]s); err != nil {
			return err
		}
	}
	if err := %[6]s.FinishSet(%[4]s); err != nil {
		return err
	}`, keyName, instName, keyTargetName, setTargetName,
			genEncRef(data, t.Key(), keyName, keyTargetName, ttSuffix+".Key()", varCount),
			targetName)
		return s

	case vdl.Map:
		mapTargetName := createUniqueName("mapTarget", varCount)
		s := fmt.Sprintf(`
	%[1]s, err := %[2]s.StartMap(%[3]s, len(%[4]s))`, mapTargetName, targetName, ttVar, instName)
		keyTargetName := createUniqueName("keyTarget", varCount)
		keyName := createUniqueName("key", varCount)
		valueTargetName := createUniqueName("valueTarget", varCount)
		valueName := createUniqueName("value", varCount)
		s += fmt.Sprintf(`
	if err != nil {
		return err
	}
	for %[1]s, %[2]s := range %[3]s {
		%[4]s, err := %[5]s.StartKey()
		if err != nil {
			return err
		}
		%[6]s
		%[7]s, err := %[5]s.FinishKeyStartField(%[4]s)
		if err != nil {
			return err
		}
		%[8]s
		if err := %[5]s.FinishField(%[4]s, %[7]s); err != nil {
			return err
		}
	}
	if err := %[9]s.FinishMap(%[5]s); err != nil {
		return err
	}`, keyName, valueName, instName, keyTargetName, mapTargetName,
			genEncRef(data, t.Key(), keyName, keyTargetName, ttSuffix+".Key()", varCount),
			valueTargetName,
			genEncRef(data, t.Elem(), valueName, valueTargetName, ttSuffix+".Elem()", varCount),
			targetName)
		return s

	case vdl.Struct:
		fieldsTargetName := createUniqueName("fieldsTarget", varCount)
		var s string
		s += fmt.Sprintf(`
	%[1]s, err := %[2]s.StartFields(%[3]s)
	if err != nil {
	return err
	}`, fieldsTargetName, targetName, ttVar)
		for ix := 0; ix < t.NumField(); ix++ {
			f := t.Field(ix)
			fieldInstName := fmt.Sprintf("%s.%s", instName, f.Name)
			fieldInstName, nativeConvBody := encWiretypeInstName(data, f.Type, fieldInstName, varCount)
			s += nativeConvBody
			keyTargetName := createUniqueName("keyTarget", varCount)
			fieldTargetName := createUniqueName("fieldTarget", varCount)
			outName, isZeroRefBody := genIsZeroBlockWiretype(data, f.Type, fieldInstName, varCount)
			s += isZeroRefBody
			ttSuffixF := ttSuffix + fmt.Sprintf(".Field(%d).Type", ix)
			s += fmt.Sprintf(`
		if %[4]s {
			if err := %[1]s.ZeroField(%[5]q); err != nil && err != %[6]sErrFieldNoExist {
				return err
			}
		} else {
			%[2]s, %[3]s, err := %[1]s.StartField(%[5]q)
			if err != %[6]sErrFieldNoExist {
				if err != nil {
					return err
				}
				%[7]s
				if err := %[1]s.FinishField(%[2]s, %[3]s); err != nil {
					return err
				}
			}
		}`, fieldsTargetName, keyTargetName, fieldTargetName, outName, f.Name, data.Pkg("v.io/v23/vdl"), genEncRefForWiretype(data, f.Type, fieldInstName, fieldTargetName, ttSuffixF, varCount))
		}
		s += fmt.Sprintf(`
	if err := %[1]s.FinishFields(%[2]s); err != nil {
		return err
	}`, targetName, fieldsTargetName)
		return s

	case vdl.Union:
		fieldsTargetName := createUniqueName("fieldsTarget", varCount)
		keyTargetName := createUniqueName("keyTarget", varCount)
		fieldTargetName := createUniqueName("fieldTarget", varCount)
		unionField, unionIndex := t.FieldByName(unionFieldName)
		ttSuffixF := ttSuffix + fmt.Sprintf(".Field(%d).Type", unionIndex)
		fieldInstName := fmt.Sprintf("%s.Value", instName)
		return fmt.Sprintf(`
	%[1]s, err := %[2]s.StartFields(%[3]s)
	if err != nil {
		return err
	}
	%[4]s, %[5]s, err := %[1]s.StartField(%[6]q)
	if err != nil {
		return err
	}
	%[7]s
	if err := %[1]s.FinishField(%[4]s, %[5]s); err != nil {
		return err
	}
	if err := %[2]s.FinishFields(%[1]s); err != nil {
		return err
	}
	`, fieldsTargetName, targetName, ttVar, keyTargetName, fieldTargetName, unionFieldName,
			genEncRef(data, unionField.Type, fieldInstName, fieldTargetName, ttSuffixF, varCount))
	}
	panic(fmt.Sprintf("encoder for kind %v unimplemented", t.Kind()))
}

var wireErrorType = vdl.TypeOf(vdl.WireError{})

// genEncRef generates either a reference to a named encoder or the body of an
// encoder that must be inlined (e.g. for a unnamed list)
func genEncRef(data *goData, t *vdl.Type, instName, targetName, ttSuffix string, varCount *int) string {
	wireInstName, s := encWiretypeInstName(data, t, instName, varCount)
	s += genEncRefForWiretype(data, t, wireInstName, targetName, ttSuffix, varCount)
	return s
}

// genEncRefForWiretype is the same as genRef, but it assumes that the instance name refers
// to a wiretype value.
func genEncRefForWiretype(data *goData, t *vdl.Type, wireInstName, targetName, ttSuffix string, varCount *int) string {
	origType, ttVar := t, ttVarName(ttSuffix)
	var s string

	if origType.Kind() == vdl.Optional {
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
		return s
	}

	// If this is a union value, give it the default value if it is nil.
	if t.Kind() == vdl.Union {
		unionName, def := createUniqueName("unionValue", varCount), data.Env.FindTypeDef(t)
		s += fmt.Sprintf(`
	%[1]s := %[2]s
	if %[1]s == nil {
		%[1]s = %[3]s{}
	}`, unionName, wireInstName, data.Pkg(def.File.Package.GenPath)+def.Name+t.Field(0).Name)
		wireInstName = unionName
	}

	if t.Name() != "" {
		// Call the inner encoder
		s += fmt.Sprintf(`
	if err := %[1]s.FillVDLTarget(%[2]s, %[3]s); err != nil {
		return err
	}`, wireInstName, targetName, ttVar)
	} else {
		// Generate an inline encoder
		s += genEncDefInternal(data, t, wireInstName, targetName, "", ttSuffix, varCount)
	}

	return s
}

// encWiretypeInstName ensures that inst name is a wiretype. It is a no-op for non-native types
// but generates conversion code for native types.
func encWiretypeInstName(data *goData, t *vdl.Type, instName string, varCount *int) (wiretypeInstName, body string) {
	// If this is a native type, convert it to wire type.
	if isNativeType(data.Env, t) {
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

func wiretypeLocalName(data *goData, t *vdl.Type) (pkgName, name string) {
	def := data.Env.FindTypeDef(t)
	return data.Pkg(def.File.Package.GenPath), def.Name
}

// genFromScalar generates a fromX() call corresponding with the appropriate scalar type.
func genFromScalar(data *goData, t *vdl.Type, instName, targetName, ttSuffix string, varCount *int) string {
	ttVar := ttVarName(ttSuffix)
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
	case vdl.String:
		methodName = "FromString"
		castType = "string"
	case vdl.Enum:
		return fmt.Sprintf(`if err := %[1]s.FromEnumLabel(%[2]s.String(), %[3]s); err != nil {
	return err
	}`, targetName, instName, ttVar)
	default:
		return ""
	}
	return fmt.Sprintf(`if err := %[1]s.%[2]s(%[3]s(%[4]s), %[5]s); err != nil {
	return err
}`, targetName, methodName, castType, instName, ttVar)
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

func containsNativeType(data *goData, t *vdl.Type) bool {
	isNative := !t.Walk(vdl.WalkAll, func(wt *vdl.Type) bool {
		return !isNativeType(data.Env, wt)
	})
	return isNative
}

// isNativeType returns true iff t is a native type.
func isNativeType(env *compile.Env, t *vdl.Type) bool {
	if def := env.FindTypeDef(t); def != nil {
		key := def.Name
		if t.Kind() == vdl.Optional {
			key = "*" + key
		}
		_, ok := def.File.Package.Config.Go.WireToNativeTypes[key]
		return ok
	}
	return false
}

func createUniqueName(prefix string, varCount *int) string {
	*varCount++
	return fmt.Sprintf("%s%d", prefix, *varCount)
}

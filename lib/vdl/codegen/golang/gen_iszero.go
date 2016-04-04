// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"
	"strings"

	"v.io/v23/vdl"
)

// genIsZeroBlock generates go code that checks if the input is zero.
func genIsZeroBlock(data *goData, t *vdl.Type, inName string, varCount *int) (outName, body string) {
	wireInName, body := isZeroWiretypeInstName(data, t, inName, varCount)
	outName, isZeroBody := genIsZeroBlockWiretype(data, t, wireInName, varCount)
	body += isZeroBody
	return outName, body
}

// genIsZeroBlockWiretype is the same as genIsZeroRef, but it assumes that the instance name refers
// to a wiretype value.
func genIsZeroBlockWiretype(data *goData, t *vdl.Type, inName string, varCount *int) (outName, body string) {
	outName = createUniqueName("var", varCount)
	if t.Kind() == vdl.Any {
		if shouldUseVdlValueForAny(data.Package) {
			body = fmt.Sprintf("\n%[1]s := %[2]s == nil || (%[2]s.Kind() == %[3]sAny && %[2]s.IsZero())", outName, inName, data.Pkg("v.io/v23/vdl"))
		} else {
			body = fmt.Sprintf("\n%[1]s := %[2]s == nil || %[2]s.IsNilAny()", outName, inName)
		}
		return
	}
	if isDirectComparable(t) {
		body = fmt.Sprintf("\n%s := (%s == %s)", outName, inName, typedConstWire(data, vdl.ZeroValue(t)))
		return
	}
	switch t.Kind() {
	case vdl.List, vdl.Set, vdl.Map:
		body = fmt.Sprintf(`
	var %[1]s bool
	if len(%[2]s) == 0 {
		%[1]s = true
	}`, outName, inName)
	case vdl.Array:
		if t.IsBytes() {
			body = fmt.Sprintf("\n%[1]s := bytes.Equal(%[2]s, %[3]s)", outName, inName, typedConstWire(data, vdl.ZeroValue(t)))
		} else {
			elemInName := createUniqueName("elem", varCount)
			elemOutName, elemBody := genIsZeroBlock(data, t.Elem(), elemInName, varCount)
			body = fmt.Sprintf(`
	%[1]s := true
	for _, %[2]s := range %[3]s {
		%[4]s
		if !%[5]s {
			%[1]s = false
			break
		}
	}`, outName, elemInName, inName, elemBody, elemOutName)
		}
	case vdl.Struct:
		body = fmt.Sprintf("\n%s := true", outName)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fOutName, fBody := genIsZeroBlock(data, f.Type, fmt.Sprintf("%s.%s", inName, f.Name), varCount)
			body += fBody
			body += fmt.Sprintf("\n%[1]s = %[1]s && %[2]s", outName, fOutName)
		}
	case vdl.Union:
		fOutName, fBody := genIsZeroBlock(data, t.Field(0).Type, "field.Value", varCount)
		def := data.Env.FindTypeDef(t)
		body = fmt.Sprintf(`
	var %[1]s bool
	if field, ok := %[2]s.(%[7]s%[3]s%[4]s); ok {
		%[5]s
		%[1]s = %[6]s
	}`, outName, inName, def.Name, t.Field(0).Name, fBody, fOutName, data.Pkg(def.File.Package.GenPath))
	case vdl.TypeObject:
		body = fmt.Sprintf("\n%[1]s := (%[2]s == nil || %[2]s == %[3]s)", outName, inName, typedConstWire(data, vdl.ZeroValue(t)))
		return
	default:
		panic(fmt.Sprintf("unexpected type: %v", t))
	}
	return
}

// isDirectComparable determines if it is possible to directly apply go's == operator
// to determine equality.
func isDirectComparable(t *vdl.Type) bool {
	switch t.Kind() {
	case vdl.Optional:
		return true
	case vdl.TypeObject, vdl.Union:
		return false
	}
	return !t.ContainsKind(vdl.WalkAll, vdl.List, vdl.Map, vdl.Set)
}

// isZeroWiretypeInstName ensures that inst name is a wiretype. It is a no-op for non-native types
// but generates conversion code for native types.
func isZeroWiretypeInstName(data *goData, t *vdl.Type, instName string, varCount *int) (wiretypeInstName, body string) {
	return wiretypeInstNameInternal(data, t, instName, fmt.Sprintf(`return fmt.Errorf("error converting %v to wiretype", )`, instName), varCount)
}

func wiretypeInstNameInternal(data *goData, t *vdl.Type, instName, failureLine string, varCount *int) (wiretypeInstName, body string) {
	// If this is a native type, convert it to wire type.
	pkgPath, name := vdl.SplitIdent(t.Name())
	if isNativeType(data.Env, t) {
		wirePkgPath := pkgPath
		if !strings.ContainsRune(pkgPath, '/') {
			wirePkgPath = "v.io/v23/vdlroot/" + pkgPath
		}
		wirePkgName := data.Pkg(wirePkgPath)

		wiretypeInstName = createUniqueName("wireValue", varCount)
		var wireType = wirePkgName + name // TODO(bprosnitz) What should this be?!
		body = fmt.Sprintf(`
	var %s %s
	if err := %s%sFromNative(&%s, %s); err != nil {
		%s
	}
	`, wiretypeInstName, wireType, wirePkgName, name, wiretypeInstName, instName, failureLine)
		instName = wiretypeInstName
		return
	}

	// If this isn't native, just return the original instName.
	return instName, ""
}

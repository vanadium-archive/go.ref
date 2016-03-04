// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"
	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
)

// genIsZeroDef generates a IsZero function for the provided type
//
// unionFieldName is the name of the union field (corresponding to the struct
// that the encode method will be on) if the type is a union
func genIsZeroDef(data goData, t *vdl.Type, unionFieldName string) string {
	var varCount int
	outName, body := genIsZeroDefInternal(data, t, "m", unionFieldName, &varCount)
	return fmt.Sprintf("\n%s\nreturn %s", body, outName)
}

func genIsZeroDefInternal(data goData, t *vdl.Type, inName, unionFieldName string, varCount *int) (outName, body string) {
	outName = createUniqueName("var", varCount)
	pkgPath, name := vdl.SplitIdent(t.Name())
	_, ok := lookupNativeGoTypes(pkgPath, name, data.File.Package, make(map[*compile.Package]bool))
	if ok {
		// It is not currently possible to directly check that an arbitrary native type's
		// corresponding wire type is zero, so  convert to vdl.Value and test it for zero.
		body = fmt.Sprintf("\n%s := %sValueOf(%s).IsZero()", outName, data.Pkg("v.io/v23/vdl"), inName)
		return
	}
	if isDirectComparable(t) && t.Kind() != vdl.Union {
		if t.Kind() == vdl.Struct {
			body = fmt.Sprintf("\n%s := (*%s == %s)", outName, inName, typedConst(data, vdl.ZeroValue(t)))
		} else {
			body = fmt.Sprintf("\n%s := (%s == %s)", outName, inName, typedConst(data, vdl.ZeroValue(t)))
		}
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
			body = fmt.Sprintf("\n%[1]s := bytes.Equal(%[2]s, %[3]s)", outName, inName, typedConst(data, vdl.ZeroValue(t)))
		} else {
			elemInName := createUniqueName("elem", varCount)
			elemOutName, elemBody := genIsZeroRef(data, t, elemInName, varCount)
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
			fOutName, fBody := genIsZeroRef(data, f.Type, fmt.Sprintf("%s.%s", inName, f.Name), varCount)
			body += fBody
			body += fmt.Sprintf("\n%[1]s = %[1]s && %[2]s", outName, fOutName)
		}
	case vdl.Union:
		f, index := t.FieldByName(unionFieldName)
		if index == 0 {
			outName, body = genIsZeroRef(data, f.Type, inName+".Value", varCount)
		} else {
			outName = createUniqueName("unionField", varCount)
			body = fmt.Sprintf("\n%s := false", outName)
		}
	default:
		panic(fmt.Sprintf("unexpected type: %v", t))
	}
	return
}

// genIsZeroRef generates either a call to a IsZero() method or the body of a IsZero() check
// that can be inlined.
func genIsZeroRef(data goData, t *vdl.Type, inName string, varCount *int) (outName, body string) {
	wireInName, body := isZeroWiretypeInstName(data, t, inName, varCount)
	outName, isZeroBody := genIsZeroRefForWiretype(data, t, wireInName, varCount)
	body += isZeroBody
	return outName, body
}

// genIsZeroRefForWiretype is the same as genIsZeroRef, but it assumes that the instance name refers
// to a wiretype value.
func genIsZeroRefForWiretype(data goData, t *vdl.Type, inName string, varCount *int) (outName, body string) {
	switch t.Kind() {
	case vdl.List, vdl.Map, vdl.Set, vdl.Optional, vdl.Any:
		// These types can be used for breaking recursion because they can be nil
		// or empty. To avoid an infinite loop with IsZero() calls, check these
		// types inline.
	default:
		if t.Name() != "" {
			outName = createUniqueName("var", varCount)
			body = fmt.Sprintf("\n%s := %s.IsZero()", outName, inName)
			return
		}
	}
	return genIsZeroDefInternal(data, t, inName, "", varCount)
}

// isDirectComparable determines if it is possible to directly apply go's == operator
// to determine equality.
func isDirectComparable(t *vdl.Type) bool {
	if t.Kind() == vdl.Any || t.Kind() == vdl.Optional {
		return true
	}
	return !t.ContainsKind(vdl.WalkAll, vdl.List, vdl.Map, vdl.Set)
}

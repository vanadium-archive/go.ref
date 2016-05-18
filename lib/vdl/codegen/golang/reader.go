// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
)

// defineRead returns VDLRead method for the def type.
func defineRead(data *goData, def *compile.TypeDef) string {
	g := genRead{goData: data}
	return g.Gen(def)
}

type genRead struct {
	*goData
	// anonReaders holds the anonymous types that we need to generate __VDLRead
	// functions for.  We only generate the function if we're the first one to add
	// it to goData; otherwise someone else has already generated it.
	anonReaders []*vdl.Type
}

func (g *genRead) Gen(def *compile.TypeDef) string {
	var s string
	if def.Type.Kind() == vdl.Union {
		s += g.genUnionDef(def)
	} else {
		s += g.genDef(def)
	}
	s += g.genAnonDef()
	return s
}

func (g *genRead) genDef(def *compile.TypeDef) string {
	s := fmt.Sprintf(`
func (x *%[1]s) VDLRead(dec %[2]sDecoder) error {`, def.Name, g.Pkg("v.io/v23/vdl"))
	// Structs need to be zeroed, since some of the fields may be missing.
	if def.Type.Kind() == vdl.Struct {
		s += fmt.Sprintf(`
	*x = %[1]s`, typedConstWire(g.goData, vdl.ZeroValue(def.Type)))
	}
	s += g.body(def.Type, namedArg{"x", true}, true) + `
}
`
	return s
}

// genUnionDef is a special-case, since we can't attach methods to a pointer
// receiver interface.  Instead we create a package-level function.
func (g *genRead) genUnionDef(def *compile.TypeDef) string {
	s := fmt.Sprintf(`
func %[1]s(dec %[2]sDecoder, x *%[3]s) error {
	if err := dec.StartValue(%[4]s); err != nil {
		return err
	}`, unionReadFuncName(def), g.Pkg("v.io/v23/vdl"), def.Name, g.TypeOf(def.Type))
	s += g.bodyUnion(def.Type, namedArg{"x", true}) + `
	return dec.FinishValue()
}
`
	return s
}

func unionReadFuncName(def *compile.TypeDef) string {
	if def.Exported {
		return "VDLRead" + def.Name
	}
	return "__VDLRead_" + def.Name
}

func (g *genRead) genAnonDef() string {
	var s string
	// Generate the __VDLRead functions for anonymous types.  Creating the
	// function for one type may cause us to need more, e.g. [][]Certificate.  So
	// we just keep looping until there are no new functions to generate.  There's
	// no danger of infinite looping, since cyclic anonymous types are disallowed
	// in the VDL type system.
	for len(g.anonReaders) > 0 {
		anons := g.anonReaders
		g.anonReaders = nil
		for _, anon := range anons {
			s += fmt.Sprintf(`
func %[1]s(dec %[2]sDecoder, x *%[3]s) error {`, g.anonReaderName(anon), g.Pkg("v.io/v23/vdl"), typeGoWire(g.goData, anon))
			s += g.body(anon, namedArg{"x", true}, true) + `
}
`
		}
	}
	return s
}

func (g *genRead) body(tt *vdl.Type, arg namedArg, topLevel bool) string {
	kind := tt.Kind()
	sta := fmt.Sprintf(`
	if err := dec.StartValue(%[1]s); err != nil {
		return err
	}`, g.TypeOf(tt))
	fin := `
	if err := dec.FinishValue(); err != nil {
		return err
	}`
	if topLevel {
		fin = `
	return dec.FinishValue()`
	}
	// Handle special cases.  The ordering of the cases is very important.
	switch {
	case tt == vdl.ErrorType:
		// Error types call verror.VDLRead directly, similar to named types, but
		// even more special-cased.  Appears before optional, since ErrorType is
		// optional.
		return g.bodyError(arg)
	case kind == vdl.Optional:
		// Optional types need special nil handling.  Appears as early as possible,
		// so that all subsequent cases have optionality handled correctly.
		return sta + g.bodyOptional(tt, arg)
	case !topLevel && isNativeType(g.Env, tt):
		// Non-top-level native types need a final native conversion, while
		// top-level native types use the regular logic to create VDLRead for the
		// wire type.  Appears as early as possible, so that all subsequent cases
		// have nativity handled correctly.
		return g.bodyNative(tt, arg)
	case !topLevel && tt.Name() != "":
		// Non-top-level named types call the VDLRead method defined on the arg.
		// The top-level type is always named, and needs a real body generated.
		// Appears before bytes, so that we use the defined method rather than
		// re-generating extra code.
		return g.bodyCallVDLRead(tt, arg)
	case tt.IsBytes() && tt.Elem() == vdl.ByteType:
		// Bytes use the special Decoder.DecodeBytes method.  Appears before
		// anonymous types, to avoid processing bytes as a list or array.
		return sta + g.bodyBytes(tt, arg) + fin
	case !topLevel && (kind == vdl.List || kind == vdl.Set || kind == vdl.Map):
		// Non-top-level anonymous types call the unexported __VDLRead* functions
		// generated in g.Gen, after the main VDLRead method has been generated.
		// Top-level anonymous types use the regular logic, to generate the actual
		// body of the __VDLRead* functions.
		return g.bodyAnon(tt, arg)
	}
	// Handle each kind of type.
	switch kind {
	case vdl.Bool:
		return sta + g.bodyScalar(tt, vdl.BoolType, arg, "Bool") + fin
	case vdl.String:
		return sta + g.bodyScalar(tt, vdl.StringType, arg, "String") + fin
	case vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64:
		return sta + g.bodyScalar(tt, vdl.Uint64Type, arg, "Uint", kind.BitLen()) + fin
	case vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64:
		return sta + g.bodyScalar(tt, vdl.Int64Type, arg, "Int", kind.BitLen()) + fin
	case vdl.Float32, vdl.Float64:
		return sta + g.bodyScalar(tt, vdl.Float64Type, arg, "Float", kind.BitLen()) + fin
	case vdl.TypeObject:
		return sta + g.bodyScalar(tt, vdl.TypeObjectType, arg, "TypeObject") + fin
	case vdl.Enum:
		return sta + g.bodyEnum(arg) + fin
	case vdl.Array:
		return sta + g.bodyArray(tt, arg)
	case vdl.List:
		return sta + g.bodyList(tt, arg)
	case vdl.Set, vdl.Map:
		return sta + g.bodySetMap(tt, arg)
	case vdl.Struct:
		return sta + g.bodyStruct(tt, arg)
	case vdl.Any:
		return g.bodyAny(tt, arg)
	default:
		panic(fmt.Errorf("VDLRead unhandled type %s", tt))
	}
}

func (g *genRead) bodyError(arg namedArg) string {
	return fmt.Sprintf(`
	if err := %[1]sVDLRead(dec, &%[2]s); err != nil {
		return err
	}`, g.Pkg("v.io/v23/verror"), arg.Name)
}

func (g *genRead) bodyNative(tt *vdl.Type, arg namedArg) string {
	wireArg := typedArg("wire", tt)
	wireBody := g.bodyCallVDLRead(tt, wireArg)
	return fmt.Sprintf(`
	var %[1]s %[2]s%[3]s
	if err := %[2]sToNative(%[1]s, %[4]s); err != nil {
		return err
	}`, wireArg.Name, typeGoWire(g.goData, tt), wireBody, arg.Ptr())
}

func (g *genRead) bodyCallVDLRead(tt *vdl.Type, arg namedArg) string {
	if tt.Kind() == vdl.Union {
		// Unions are a special-case, since we can't attach methods to a pointer
		// receiver interface.  Instead we call the package-level function.
		def := g.Env.FindTypeDef(tt)
		return fmt.Sprintf(`
	if err := %[1]s%[2]s(dec, %[3]s); err != nil {
		return err
	}`, g.Pkg(def.File.Package.GenPath), unionReadFuncName(def), arg.Ptr())
	}
	return fmt.Sprintf(`
	if err := %[1]s.VDLRead(dec); err != nil {
		return err
	}`, arg.Name)
}

func (g *genRead) anonReaderName(tt *vdl.Type) string {
	return fmt.Sprintf("__VDLReadAnon_%s_%d", tt.Kind(), g.goData.anonReaders[tt])
}

func (g *genRead) bodyAnon(tt *vdl.Type, arg namedArg) string {
	id := g.goData.anonReaders[tt]
	if id == 0 {
		// This is the first time we've encountered this type, add it.
		id = len(g.goData.anonReaders) + 1
		g.goData.anonReaders[tt] = id
		g.anonReaders = append(g.anonReaders, tt)
	}
	return fmt.Sprintf(`
	if err := %[1]s(dec, %[2]s); err != nil {
		return err
	}`, g.anonReaderName(tt), arg.Ptr())
}

func (g *genRead) bodyScalar(tt, exact *vdl.Type, arg namedArg, method string, params ...interface{}) string {
	var paramStr string
	for i, p := range params {
		if i > 0 {
			paramStr += ", "
		}
		paramStr += fmt.Sprint(p)
	}
	if tt == exact {
		return fmt.Sprintf(`
	var err error
	if %[1]s, err = dec.Decode%[2]s(%[3]s); err != nil {
		return err
	}`, arg.Ref(), method, paramStr)
	}
	// The types don't have an exact match, so we need a conversion.  This
	// occurs for all named types, as well as numeric types where the bitlen
	// isn't exactly the same.  E.g.
	//
	//   type Foo uint16
	//
	//   tmp, err := dec.DecodeUint(16) // returns uint64
	//   if err != nil { return err }
	//   *x = Foo(tmp)
	return fmt.Sprintf(`
	tmp, err := dec.Decode%[2]s(%[3]s)
	if err != nil {
		return err
	}
	%[1]s = %[4]s(tmp)`, arg.Ref(), method, paramStr, typeGoWire(g.goData, tt))
}

func (g *genRead) bodyEnum(arg namedArg) string {
	return fmt.Sprintf(`
	enum, err := dec.DecodeString()
	if err != nil {
		return err
	}
	if err := %[1]s.Set(enum); err != nil {
		return err
	}`, arg.Name)
}

func (g *genRead) bodyBytes(tt *vdl.Type, arg namedArg) string {
	s, decodeVar := "", arg.Ptr()
	var max int
	var assignEnd bool
	switch tt.Kind() {
	case vdl.Array:
		s += fmt.Sprintf(`
	bytes := %[1]s[:]`, arg.Name)
		decodeVar, max = "&bytes", tt.Len()
	case vdl.List:
		max = -1
		if tt.Name() != "" {
			s += `
	var bytes []byte`
			decodeVar, assignEnd = "&bytes", true
		}
	}
	s += fmt.Sprintf(`
	if err := dec.DecodeBytes(%[1]d, %[2]s); err != nil {
		return err
	}`, max, decodeVar)
	if assignEnd {
		s += fmt.Sprintf(`
	%[1]s = bytes`, arg.Ref())
	}
	return s
}

func (g *genRead) bodyArray(tt *vdl.Type, arg namedArg) string {
	elemArg := arg.ArrayIndex("index", tt.Elem())
	elemBody := g.body(tt.Elem(), elemArg, false)
	return fmt.Sprintf(`
	index := 0
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done != (index >= len(%[2]s)):
			return %[1]sErrorf("array len mismatch, done:%%v index:%%d len:%%d %%T)", done, index, len(%[2]s), %[2]s)
		case done:
			return dec.FinishValue()
		}%[3]s
		index++
	}`, g.Pkg("fmt"), arg.Ref(), elemBody)
}

func (g *genRead) bodyList(tt *vdl.Type, arg namedArg) string {
	elemArg := typedArg("elem", tt.Elem())
	elemBody := g.body(tt.Elem(), elemArg, false)
	return fmt.Sprintf(`
	switch len := dec.LenHint(); {
	case len > 0:
		%[1]s = make(%[2]s, 0, len)
	default:
		%[1]s = nil
	}
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			return dec.FinishValue()
		}
		var elem %[3]s%[4]s
		%[1]s = append(%[1]s, elem)
	}`, arg.Ref(), typeGoWire(g.goData, tt), typeGo(g.goData, tt.Elem()), elemBody)
}

func (g *genRead) bodySetMap(tt *vdl.Type, arg namedArg) string {
	keyArg := typedArg("key", tt.Key())
	keyBody := g.body(tt.Key(), keyArg, false)
	s := fmt.Sprintf(`
	var tmpMap %[1]s
	if len := dec.LenHint(); len > 0 {
		tmpMap = make(%[1]s, len)
  }
	for {
		switch done, err := dec.NextEntry(); {
		case err != nil:
			return err
		case done:
			%[2]s = tmpMap
			return dec.FinishValue()
		}
		var key %[3]s
		{%[4]s
		}`, typeGoWire(g.goData, tt), arg.Ref(), typeGo(g.goData, tt.Key()), keyBody)
	elemValue := "struct{}{}"
	if tt.Kind() == vdl.Map {
		elemValue = "elem"
		elemArg := typedArg("elem", tt.Elem())
		elemBody := g.body(tt.Elem(), elemArg, false)
		s += fmt.Sprintf(`
		var elem %[1]s
		{%[2]s
		}`, typeGo(g.goData, tt.Elem()), elemBody)
	}
	return s + fmt.Sprintf(`
		if tmpMap == nil {
			tmpMap = make(%[1]s)
		}
		tmpMap[key] = %[2]s
	}`, typeGoWire(g.goData, tt), elemValue)
}

func (g *genRead) bodyStruct(tt *vdl.Type, arg namedArg) string {
	s := `
	for {
		f, err := dec.NextField()
		if err != nil {
			return err
		}
		switch f {
		case "":
			return dec.FinishValue()`
	for f := 0; f < tt.NumField(); f++ {
		field := tt.Field(f)
		fieldBody := g.body(field.Type, arg.Field(field), false)
		s += fmt.Sprintf(`
		case %[1]q:%[2]s`, field.Name, fieldBody)
	}
	return s + `
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}`
}

func (g *genRead) bodyUnion(tt *vdl.Type, arg namedArg) string {
	s := `
	f, err := dec.NextField()
	if err != nil {
		return err
	}
  switch f {`
	for f := 0; f < tt.NumField(); f++ {
		// TODO(toddw): Change to using pointers to the union field structs, to
		// resolve https://v.io/i/455
		field := tt.Field(f)
		fieldArg := typedArg("field.Value", field.Type)
		fieldBody := g.body(field.Type, fieldArg, false)
		s += fmt.Sprintf(`
	case %[1]q:
		var field %[2]s%[1]s%[3]s
		%[4]s = field`, field.Name, typeGoWire(g.goData, tt), fieldBody, arg.Ref())
	}
	return s + fmt.Sprintf(`
	case "":
		return %[1]sErrorf("missing field in union %%T, from %%v", %[2]s, dec.Type())
	default:
		return %[1]sErrorf("field %%q not in union %%T, from %%v", f, %[2]s, dec.Type())
	}
	switch f, err := dec.NextField(); {
	case err != nil:
		return err
	case f != "":
		return %[1]sErrorf("extra field %%q in union %%T, from %%v", f, %[2]s, dec.Type())
	}`, g.Pkg("fmt"), arg.Ptr())
}

func (g *genRead) bodyOptional(tt *vdl.Type, arg namedArg) string {
	// NOTE: arg.IsPtr is always true here, since tt is optional.
	body := g.body(tt.Elem(), arg, false)
	return fmt.Sprintf(`
	if dec.IsNil() {
		%[1]s = nil
		if err := dec.FinishValue(); err != nil {
			return err
		}
	} else {
		%[1]s = new(%[2]s)
		dec.IgnoreNextStartValue()%[3]s
	}`, arg.Name, typeGo(g.goData, tt.Elem()), body)
}

func (g *genRead) bodyAny(tt *vdl.Type, arg namedArg) string {
	var anyType string
	switch goAnyRepMode(g.Package) {
	case goAnyRepInterface:
		// Handle interface{} special-case.
		return fmt.Sprintf(`
	var readAny interface{}
	if err := %[1]sRead(dec, &readAny); err != nil {
		return err
	}
	%[2]s = readAny`, g.Pkg("v.io/v23/vdl"), arg.Ref())
	case goAnyRepRawBytes:
		anyType = g.Pkg("v.io/v23/vom") + "RawBytes"
	case goAnyRepValue:
		anyType = g.Pkg("v.io/v23/vdl") + "Value"
	}
	// Handle vdl.Value and vom.RawBytes representations, which need to be
	// allocated before we call VDLRead on them.  Even if its already allocated,
	// we need to create a new (invalid) vdl.Value here, to cause VDLRead to fill
	// the value with the exact type read from the decoder.
	return fmt.Sprintf(`
	%[1]s = new(%[2]s)%[3]s`, arg.Ref(), anyType, g.bodyCallVDLRead(tt, arg))
}

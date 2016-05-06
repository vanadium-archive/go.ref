// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
)

// defineWrite returns the VDLWrite method for the def type.
func defineWrite(data *goData, def *compile.TypeDef) string {
	g := genWrite{goData: data}
	return g.Gen(def)
}

type genWrite struct {
	*goData
	// anonWriters holds the anonymous types that we need to generate __VDLWrite
	// functions for.  We only generate the function if we're the first one to add
	// it to goData; otherwise someone else has already generated it.
	anonWriters []*vdl.Type
}

func (g *genWrite) Gen(def *compile.TypeDef) string {
	var s string
	if def.Type.Kind() == vdl.Union {
		s += g.genUnionDef(def)
	} else {
		s += g.genDef(def)
	}
	s += g.genAnonDef()
	return s
}

func (g *genWrite) genDef(def *compile.TypeDef) string {
	body := g.body(def.Type, namedArg{"x", false}, false, true)
	return fmt.Sprintf(`
func (x %[1]s) VDLWrite(enc %[2]sEncoder) error {%[3]s
}
`, def.Name, g.Pkg("v.io/v23/vdl"), body)
}

// genUnionDef is a special-case, since we need to generate methods for each
// concrete union struct.
func (g *genWrite) genUnionDef(def *compile.TypeDef) string {
	var s string
	for ix := 0; ix < def.Type.NumField(); ix++ {
		field := def.Type.Field(ix)
		unionType := typedConst(g.goData, vdl.TypeObjectValue(def.Type))
		body := g.bodyUnion(field, namedArg{"x", false})
		s += fmt.Sprintf(`
func (x %[1]s%[2]s) VDLWrite(enc %[3]sEncoder) error {
	if err := enc.StartValue(%[4]s); err != nil {
		return err
	}%[5]s
	return enc.FinishValue()
}
`, def.Name, field.Name, g.Pkg("v.io/v23/vdl"), unionType, body)
	}
	return s
}

func (g *genWrite) genAnonDef() string {
	var s string
	// Generate the __VDLWrite functions for anonymous types.  Creating the
	// function for one type may cause us to need more, e.g. [][]Certificate.  So
	// we just keep looping until there are no new functions to generate.  There's
	// no danger of infinite looping, since cyclic anonymous types are disallowed
	// in the VDL type system.
	for len(g.anonWriters) > 0 {
		anons := g.anonWriters
		g.anonWriters = nil
		for _, anon := range anons {
			body := g.body(anon, namedArg{"x", false}, false, true)
			s += fmt.Sprintf(`
func %[1]s(enc %[2]sEncoder, x %[3]s) error {%[4]s
}
`, g.anonWriterName(anon), g.Pkg("v.io/v23/vdl"), typeGo(g.goData, anon), body)
		}
	}
	return s
}

func (g *genWrite) body(tt *vdl.Type, arg namedArg, skipNilCheck, topLevel bool) string {
	kind := tt.Kind()
	sta := fmt.Sprintf(`
	if err := enc.StartValue(%[1]s); err != nil {
		return err
	}`, typedConst(g.goData, vdl.TypeObjectValue(tt)))
	fin := `
	if err := enc.FinishValue(); err != nil {
		return err
	}`
	if topLevel {
		fin = `
	return enc.FinishValue()`
	}
	// Handle special cases.  The ordering of the cases is very important.
	switch {
	case tt == vdl.ErrorType:
		// Error types call verror.VDLWrite directly, similar to named types, but
		// even more special-cased.  Appears before optional, since ErrorType is
		// optional.
		return g.bodyError(arg)
	case kind == vdl.Optional:
		// Optional types need special nil handling.  Appears before native types,
		// to allow native types to be optional.
		return g.bodyOptional(tt, arg, skipNilCheck)
	case !topLevel && isNativeType(g.Env, tt):
		// Non-top-level native types need an initial native conversion, while
		// top-level native types use the regular logic to create VDLWrite for the
		// wire type.  Appears as early as possible, so that all subsequent cases
		// have nativity handled correctly.
		return g.bodyNative(tt, arg, skipNilCheck)
	case !topLevel && tt.Name() != "":
		// Non-top-level named types call the VDLWrite method defined on the arg.
		// The top-level type is always named, and needs a real body generated.
		// Appears before bytes, so that we use the defined method rather than
		// re-generating extra code.
		return g.bodyCallVDLWrite(tt, arg, skipNilCheck)
	case tt.IsBytes() && tt.Elem() == vdl.ByteType:
		// Bytes use the special Encoder.EncodeBytes method.  Appears before
		// anonymous types, to avoid processing bytes as a list or array.
		return sta + g.bodyBytes(tt, arg) + fin
	case !topLevel && (kind == vdl.List || kind == vdl.Set || kind == vdl.Map):
		// Non-top-level anonymous types call the unexported __VDLWrite* functions
		// generated in g.Gen, after the main VDLWrite method has been generated.
		// Top-level anonymous types use the regular logic, to generate the actual
		// body of the __VDLWrite* functions.
		return g.bodyAnon(tt, arg)
	}
	// Handle each kind of type.
	switch kind {
	case vdl.Bool:
		return sta + g.bodyScalar(tt, vdl.BoolType, arg, "Bool") + fin
	case vdl.String:
		return sta + g.bodyScalar(tt, vdl.StringType, arg, "String") + fin
	case vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64:
		return sta + g.bodyScalar(tt, vdl.Uint64Type, arg, "Uint") + fin
	case vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64:
		return sta + g.bodyScalar(tt, vdl.Int64Type, arg, "Int") + fin
	case vdl.Float32, vdl.Float64:
		return sta + g.bodyScalar(tt, vdl.Float64Type, arg, "Float") + fin
	case vdl.TypeObject:
		return g.bodyCallVDLWrite(tt, arg, skipNilCheck)
	case vdl.Enum:
		return sta + g.bodyEnum(tt, arg) + fin
	case vdl.Array:
		return sta + g.bodyArray(tt, arg) + fin
	case vdl.List:
		return sta + g.bodyList(tt, arg) + fin
	case vdl.Set:
		return sta + g.bodySet(tt, arg) + fin
	case vdl.Map:
		return sta + g.bodyMap(tt, arg) + fin
	case vdl.Struct:
		return sta + g.bodyStruct(tt, arg) + fin
	case vdl.Any:
		return g.bodyAny(arg, skipNilCheck)
	default:
		panic(fmt.Errorf("VDLWrite unhandled type %s", tt))
	}
}

func (g *genWrite) bodyError(arg namedArg) string {
	return fmt.Sprintf(`
	if err := %[1]sVDLWrite(enc, %[2]s); err != nil {
		return err
	}`, g.Pkg("v.io/v23/verror"), arg.Name)
}

func (g *genWrite) bodyNative(tt *vdl.Type, arg namedArg, skipNilCheck bool) string {
	s := fmt.Sprintf(`
	var wire %[1]s
	if err := %[1]sFromNative(&wire, %[2]s); err != nil {
		return err
	}`, typeGoWire(g.goData, tt), arg.Ref())
	return s + g.bodyCallVDLWrite(tt, typedArg("wire", tt), skipNilCheck)
}

func (g *genWrite) bodyCallVDLWrite(tt *vdl.Type, arg namedArg, skipNilCheck bool) string {
	s := fmt.Sprintf(`
	if err := %[1]s.VDLWrite(enc); err != nil {
		return err
	}`, arg.Name)
	// Handle cases where a nil arg would cause the VDLWrite call to panic.  Here
	// are the potential cases:
	//   Optional:       Never happens; optional types already handled.
	//   TypeObject:     The vdl.Type.VDLWrite method handles nil.
	//   List, Set, Map: VDLWrite uses len(arg) and "range arg", which handle nil.
	//   Union:          Needs handling below.
	//   Any:            Needs handling below.
	if k := tt.Kind(); !skipNilCheck && (k == vdl.Union || k == vdl.Any) {
		ttType := typedConst(g.goData, vdl.TypeObjectValue(tt))
		s += fmt.Sprintf(`
	switch {
	case %[1]s == nil:
		// Write the zero value of the %[2]s type.
		if err := %[3]sZeroValue(%[4]s).VDLWrite(enc); err != nil {
			return err
		}
	default:%[5]s
	}`, arg.Ref(), k.String(), g.Pkg("v.io/v23/vdl"), ttType, s)
	}
	return s
}

func (g *genWrite) anonWriterName(tt *vdl.Type) string {
	return fmt.Sprintf("__VDLWriteAnon_%s_%d", tt.Kind(), g.goData.anonWriters[tt])
}

func (g *genWrite) bodyAnon(tt *vdl.Type, arg namedArg) string {
	id := g.goData.anonWriters[tt]
	if id == 0 {
		// This is the first time we've encountered this type, add it.
		id = len(g.goData.anonWriters) + 1
		g.goData.anonWriters[tt] = id
		g.anonWriters = append(g.anonWriters, tt)
	}
	return fmt.Sprintf(`
	if err := %[1]s(enc, %[2]s); err != nil {
		return err
	}`, g.anonWriterName(tt), arg.Ref())
}

func (g *genWrite) bodyScalar(tt, exact *vdl.Type, arg namedArg, method string) string {
	param := arg.Ref()
	if tt != exact {
		param = typeGo(g.goData, exact) + "(" + arg.Ref() + ")"
	}
	return fmt.Sprintf(`
	if err := enc.Encode%[1]s(%[2]s); err != nil {
		return err
	}`, method, param)
}

var ttBytes = vdl.ListType(vdl.ByteType)

func (g *genWrite) bodyBytes(tt *vdl.Type, arg namedArg) string {
	if tt.Kind() == vdl.Array {
		arg = arg.ArrayIndex(":", ttBytes)
	}
	return g.bodyScalar(tt, ttBytes, arg, "Bytes")
}

func (g *genWrite) bodyEnum(tt *vdl.Type, arg namedArg) string {
	return fmt.Sprintf(`
	if err := enc.EncodeString(%[1]s.String()); err != nil {
		return err
	}`, arg.Name)
}

const (
	encNextEntry = `
	if err := enc.NextEntry(false); err != nil {
		return err
	}`
	encNextEntryDone = `
	if err := enc.NextEntry(true); err != nil {
		return err
	}`
	encNextFieldDone = `
	if err := enc.NextField(""); err != nil {
		return err
	}`
)

func (g *genWrite) bodyArray(tt *vdl.Type, arg namedArg) string {
	elemArg := arg.ArrayIndex("i", tt.Elem())
	s := fmt.Sprintf(`
	for i := 0; i < %[1]d; i++ {`, tt.Len())
	s += encNextEntry
	s += g.body(tt.Elem(), elemArg, false, false)
	s += `
	}` + encNextEntryDone
	return s
}

func (g *genWrite) bodyList(tt *vdl.Type, arg namedArg) string {
	elemArg := arg.Index("i", tt.Elem())
	s := fmt.Sprintf(`
	if err := enc.SetLenHint(len(%[1]s)); err != nil {
		return err
	}
	for i := 0; i < len(%[1]s); i++ {`, arg.Ref())
	s += encNextEntry
	s += g.body(tt.Elem(), elemArg, false, false)
	s += `
	}` + encNextEntryDone
	return s
}

func (g *genWrite) bodySet(tt *vdl.Type, arg namedArg) string {
	keyArg := typedArg("key", tt.Key())
	s := fmt.Sprintf(`
	if err := enc.SetLenHint(len(%[1]s)); err != nil {
		return err
	}
	for key := range %[1]s {`, arg.Ref())
	s += encNextEntry
	s += g.body(tt.Key(), keyArg, false, false)
	s += `
	}` + encNextEntryDone
	return s
}

func (g *genWrite) bodyMap(tt *vdl.Type, arg namedArg) string {
	keyArg, elemArg := typedArg("key", tt.Key()), typedArg("elem", tt.Elem())
	s := fmt.Sprintf(`
	if err := enc.SetLenHint(len(%[1]s)); err != nil {
		return err
	}
	for key, elem := range %[1]s {`, arg.Ref())
	s += encNextEntry
	s += g.body(tt.Key(), keyArg, false, false)
	s += g.body(tt.Elem(), elemArg, false, false)
	s += `
	}` + encNextEntryDone
	return s
}

func (g *genWrite) bodyStruct(tt *vdl.Type, arg namedArg) string {
	var s string
	for i := 0; i < tt.NumField(); i++ {
		field := tt.Field(i)
		fieldArg := arg.Field(field)
		zero := genIsZero{g.goData}
		expr := zero.Expr(ifNeZero, field.Type, fieldArg, field.Name)
		// The second-to-last true parameter indicates that nil checks can be
		// skipped, since we've already ensured the field isn't zero here.
		fieldBody := g.body(field.Type, fieldArg, true, false)
		s += fmt.Sprintf(`
	if %[1]s {
		if err := enc.NextField(%[2]q); err != nil {
			return err
		}%[3]s
	}`, expr, field.Name, fieldBody)
	}
	s += encNextFieldDone
	return s
}

func (g *genWrite) bodyUnion(field vdl.Field, arg namedArg) string {
	fieldArg := typedArg(arg.Name+".Value", field.Type)
	s := fmt.Sprintf(`
	if err := enc.NextField(%[1]q); err != nil {
			return err
	}`, field.Name)
	s += g.body(field.Type, fieldArg, false, false)
	s += encNextFieldDone
	return s
}

func (g *genWrite) bodyOptional(tt *vdl.Type, arg namedArg, skipNilCheck bool) string {
	body := g.body(tt.Elem(), arg, false, false)
	s := fmt.Sprintf(`
	enc.SetNextStartValueIsOptional()
	%[1]s
	`, body)
	if !skipNilCheck {
		s = fmt.Sprintf(`
	if %[1]s == nil {
		if err := enc.NilValue(%[2]s); err != nil {
			return err
		}
	} else {%[3]s
	}`, arg.Name, typedConst(g.goData, vdl.TypeObjectValue(tt)), s)
	}
	return s
}

func (g *genWrite) bodyAny(arg namedArg, skipNilCheck bool) string {
	mode := goAnyRepMode(g.Package)
	// Handle interface{} special-case.
	if mode == goAnyRepInterface {
		return fmt.Sprintf(`
	if err := %[1]sWrite(enc, %[2]s); err != nil {
		return err
	}`, g.Pkg("v.io/v23/vdl"), arg.Ref())
	}
	// Handle vdl.Value and vom.RawBytes representations.
	s := fmt.Sprintf(`
	if err := %[1]s.VDLWrite(enc); err != nil {
		return err
	}`, arg.Name)
	if !skipNilCheck {
		s = fmt.Sprintf(`
	if %[1]s == nil {
		if err := enc.NilValue(%[2]sAnyType); err != nil {
			return err
		}
	} else {%[3]s
	}`, arg.Ref(), g.Pkg("v.io/v23/vdl"), s)
	}
	return s
}

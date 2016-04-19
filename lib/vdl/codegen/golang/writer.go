// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
)

// writerGo generates the VDLWrite method for the def type.
func writerGo(data *goData, def *compile.TypeDef) string {
	g := genWrite{goData: data}
	return g.Gen(def)
}

type genWrite struct {
	*goData
	// anonWriters holds the anonymous types that we need to generate __VDLWrite
	// functions for.  We only generate the function if we're the first one to add
	// it to goData; otherwise someone else has already generated it.
	anonWriters []*vdl.Type
	varCount    int // TODO(bprosnitz) This is for isZero due to the current impl, switch to namedArg
}

func (g *genWrite) Gen(def *compile.TypeDef) string {
	var s string
	if def.Type.Kind() == vdl.Union {
		for i := 0; i < def.Type.NumField(); i++ {
			s += g.genUnionDef(def, def.Type.Field(i))
		}
	} else {
		s += g.genDef(def)
	}
	s += g.genAnon()
	return s
}

func (g *genWrite) genDef(def *compile.TypeDef) string {
	s := fmt.Sprintf(`
func (x %[1]s) VDLWrite(enc %[2]sEncoder) error {`, def.Name, g.Pkg("v.io/v23/vdl"))
	s += g.body(def.Type, namedArg{"x", false}, true, "") + `
}`
	return s
}

func (g *genWrite) genUnionDef(def *compile.TypeDef, field vdl.Field) string {
	s := fmt.Sprintf(`
func (x %[1]s%[2]s) VDLWrite(enc %[3]sEncoder) error {`, def.Name, field.Name, g.Pkg("v.io/v23/vdl"))
	s += g.body(def.Type, namedArg{"x", false}, true, field.Name) + `
}`
	return s
}

func (g *genWrite) genAnon() string {
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
			s += fmt.Sprintf(`

func %[1]s(enc %[2]sEncoder, x *%[3]s) error {`, g.anonWriterName(anon), g.Pkg("v.io/v23/vdl"), typeGo(g.goData, anon))
			s += g.body(anon, namedArg{"x", true}, true, "") + `
}`
		}
	}
	return s
}

func (g *genWrite) body(tt *vdl.Type, arg namedArg, topLevel bool, unionField string) string {
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
		// Optional types need special nil handling.  Appears as early as possible,
		// so that all subsequent cases have optionality handled correctly.
		return sta + g.bodyOptional(tt, arg)
	case !topLevel && isNativeType(g.Env, tt):
		// Non-top-level native types need an initial native conversion, while
		// top-level native types use the regular logic to create VDLWrite for the
		// wire type.  Appears as early as possible, so that all subsequent cases
		// have nativity handled correctly.
		return g.bodyNative(tt, arg)
	case !topLevel && tt.Name() != "":
		// Non-top-level named types call the VDLWrite method defined on the arg.
		// The top-level type is always named, and needs a real body generated.
		// Appears before bytes, so that we use the defined method rather than
		// re-generating extra code.
		return g.bodyCallVDLWrite(tt, arg)
	case tt.IsBytes():
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
		return sta + g.bodyTypeObject(tt, arg) + fin
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
	case vdl.Union:
		return sta + g.bodyUnion(tt, unionField, arg) + fin
	case vdl.Any:
		return sta + g.bodyAny(arg) + fin
	default:
		panic(fmt.Errorf("vom: unhandled type %s", tt))
	}
}

func (g *genWrite) bodyError(arg namedArg) string {
	return fmt.Sprintf(`
	if err := %[1]sVDLWrite(enc, %[2]s); err != nil {
		return err
	}`, g.Pkg("v.io/v23/verror"), arg.Name)
}

func (g *genWrite) bodyNative(tt *vdl.Type, arg namedArg) string {
	s := fmt.Sprintf(`
	var wire %[1]s
  	if err := %[1]sFromNative(&wire, %[2]s); err != nil {
    		return err
  	}`, typeGoWire(g.goData, tt), arg.Ref())
	s += g.bodyCallVDLWrite(tt, typedArg("wire", tt))
	return s
}

func (g *genWrite) bodyCallVDLWrite(tt *vdl.Type, arg namedArg) string {
	return fmt.Sprintf(`
	if err := %[1]s.VDLWrite(enc); err != nil {
		return err
	}`, arg.Name)
}

func (g *genWrite) anonWriterName(tt *vdl.Type) string {
	return fmt.Sprintf("__VDLWrite%d_%s", g.goData.anonWriters[tt], tt.Kind())
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
	}`, g.anonWriterName(tt), arg.Ptr())
}

func (g *genWrite) bodyScalar(tt *vdl.Type, exact *vdl.Type, arg namedArg, method string) string {
	if exact == tt {
		return fmt.Sprintf(`
	if err := enc.Encode%[1]s(%[2]s); err != nil {
		return err
	}`, method, arg.Ref())
	} else {
		return fmt.Sprintf(`
	if err := enc.Encode%[1]s(%[2]s(%[3]s)); err != nil {
		return err
	}`, method, typeGo(g.goData, exact), arg.Ref())
	}
}

var ttBytes *vdl.Type = vdl.TypeOf([]byte{})

func (g *genWrite) bodyBytes(tt *vdl.Type, arg namedArg) string {
	if tt.Kind() == vdl.Array {
		arg = typedArg(fmt.Sprintf("%s[:]", arg.SafeRef()), ttBytes)
	}
	return g.bodyScalar(tt, ttBytes, arg, "Bytes")
}

func (g *genWrite) bodyTypeObject(tt *vdl.Type, arg namedArg) string {
	return fmt.Sprintf(`
	if err := enc.EncodeTypeObject(%[1]s); err != nil {
		return err
	}`, arg.Ref())
}

func (g *genWrite) bodyEnum(tt *vdl.Type, arg namedArg) string {
	return fmt.Sprintf(`
	if err := enc.EncodeString(%[1]s.String()); err != nil {
		return err
	}`, arg.Name)
}

func (g *genWrite) bodyArray(tt *vdl.Type, arg namedArg) string {
	s := fmt.Sprintf(`
	for i := 0; i < %[1]d; i++ {`, tt.Len())
	s += g.body(tt.Elem(), arg.Index("i", tt.Elem()), false, "")
	s += `
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}`
	return s
}

func (g *genWrite) bodyList(tt *vdl.Type, arg namedArg) string {
	s := fmt.Sprintf(`
	for i := 0; i < len(%[1]v); i++ {`, arg.Ref())
	s += g.body(tt.Elem(), arg.Index("i", tt.Elem()), false, "")
	s += `
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}`
	return s
}

func (g *genWrite) bodySet(tt *vdl.Type, arg namedArg) string {
	keyArg := typedArg("key", tt.Key())
	s := fmt.Sprintf(`
	for %[1]v := range %[2]v {`, keyArg.Ref(), arg.Ref())
	s += g.body(tt.Key(), keyArg, false, "")
	s += `
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}`
	return s
}

func (g *genWrite) bodyMap(tt *vdl.Type, arg namedArg) string {
	keyArg := typedArg("key", tt.Key())
	elemArg := typedArg("elem", tt.Elem())
	s := fmt.Sprintf(`
	for %[1]v, %[2]v := range %[3]v {`, keyArg.Ref(), elemArg.Ref(), arg.Ref())
	s += g.body(tt.Key(), keyArg, false, "")
	s += g.body(tt.Elem(), elemArg, false, "")
	s += `
	}
	if err := enc.NextEntry(true); err != nil {
		return err
	}`
	return s
}

func (g *genWrite) bodyStruct(tt *vdl.Type, arg namedArg) string {
	var s string
	for i := 0; i < tt.NumField(); i++ {
		field := tt.Field(i)
		isZeroVar, isZeroBody := genIsZeroBlock(g.goData, field.Type, arg.Field(field).Name, &g.varCount)
		s += isZeroBody
		s += fmt.Sprintf(`
	if !(%[1]s) {
		if err := enc.NextField(%[2]q); err != nil {
			return err
		}`, isZeroVar, field.Name)
		s += g.body(field.Type, arg.Field(field), false, "")
		s += `
	}`
	}
	s += `
	if err := enc.NextField(""); err != nil {
		return err
	}`
	return s
}

func (g *genWrite) bodyUnion(tt *vdl.Type, unionField string, arg namedArg) string {
	field, _ := tt.FieldByName(unionField)
	s := fmt.Sprintf(`
	if err := enc.NextField(%[1]q); err != nil {
			return err
	}`, unionField)
	s += g.body(field.Type, arg.Field(vdl.Field{Name: "Value", Type: field.Type}), false, "")
	s += `
	if err := enc.NextField(""); err != nil {
		return err
	}`
	return s
}

func (g *genWrite) bodyOptional(tt *vdl.Type, arg namedArg) string {
	s := fmt.Sprintf(`
	if %[1]s == nil {
		if err := enc.NilValue(%[2]s); err != nil {
			return err
		}
	} else {
		enc.SetNextStartValueIsOptional()`, arg.Ptr(), typedConst(g.goData, vdl.TypeObjectValue(tt)))
	s += g.body(tt.NonOptional(), arg, false, "")
	s += `
	}`
	return s
}

func (g *genWrite) bodyAny(arg namedArg) string {
	var typeAccessor string = "Type"
	if shouldUseVdlValueForAny(g.goData.Package) {
		typeAccessor = "Type()"
	}
	return fmt.Sprintf(`
	if %[1]s.IsNil() {
		if err := enc.NilValue(%[1]s.%[2]s); err != nil {
			return err
		}
	} else {
		if err := %[1]s.VDLWrite(enc); err != nil {
				     return err
		}
	}`, arg.Name, typeAccessor)
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"
	"strconv"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
)

func constDefGo(data *goData, def *compile.ConstDef) string {
	v := def.Value
	return fmt.Sprintf("%s%s %s = %s%s", def.Doc, constOrVar(v.Kind()), def.Name, typedConst(data, v), def.DocSuffix)
}

func constOrVar(k vdl.Kind) string {
	switch k {
	case vdl.Bool, vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64, vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64, vdl.Float32, vdl.Float64, vdl.Complex64, vdl.Complex128, vdl.String, vdl.Enum:
		return "const"
	}
	return "var"
}

func isByteList(t *vdl.Type) bool {
	return t.Kind() == vdl.List && t.Elem().Kind() == vdl.Byte
}

func genValueOf(data *goData, v *vdl.Value) string {
	// There's no need to convert the value to its native representation, since
	// it'll just be converted back in vdl.ValueOf.
	return data.Pkg("v.io/v23/vdl") + "ValueOf(" + typedConstWire(data, v) + ")"
}

func typedConst(data *goData, v *vdl.Value) string {
	if native := nativeConst(data, v); native != "" {
		return native
	}
	return typedConstWire(data, v)
}

// TODO(bprosnitz): Generate the full tag name e.g. security.Read instead of
// security.Label(1)
//
// TODO(toddw): This is broken for optional types that can't be represented
// using a composite literal (e.g. optional primitives).
//
// https://github.com/vanadium/issues/issues/213
func typedConstWire(data *goData, v *vdl.Value) string {
	k, t := v.Kind(), v.Type()
	typestr := typeGoWire(data, t)
	if k == vdl.Optional {
		if elem := v.Elem(); elem != nil {
			return "&" + typedConst(data, elem)
		}
		return "(" + typestr + ")(nil)" // results in (*Foo)(nil)
	}
	valstr := untypedConstWire(data, v)
	// Enum, TypeObject, Union, Any and non-zero []byte already include the type
	// in their "untyped" values.  Built-in bool and string are implicitly
	// convertible from their literals.
	if k == vdl.Enum || k == vdl.TypeObject || k == vdl.Any || (isByteList(t) && !v.IsZero()) || t == vdl.BoolType || t == vdl.StringType {
		return valstr
	}
	// Everything else requires an explicit type.
	// { } are used instead of ( ) for composites
	switch k {
	case vdl.Array, vdl.Struct:
		return typestr + valstr
	case vdl.List, vdl.Set, vdl.Map:
		// Special-case empty variable-length collections, which we generate as a type
		// conversion from nil
		if !v.IsZero() {
			return typestr + valstr
		}
	}
	return typestr + "(" + valstr + ")"
}

func untypedConst(data *goData, v *vdl.Value) string {
	if native := nativeConst(data, v); native != "" {
		return native
	}
	return untypedConstWire(data, v)
}

func untypedConstWire(data *goData, v *vdl.Value) string {
	k, t := v.Kind(), v.Type()
	typestr := typeGoWire(data, t)
	if isByteList(t) {
		if v.IsZero() {
			return "nil"
		}
		return typestr + "(" + strconv.Quote(string(v.Bytes())) + ")"
	}
	switch k {
	case vdl.Any:
		if elem := v.Elem(); elem != nil {
			// We need to generate a Go expression of type *vom.RawBytes or *vdl.Value
			// that represents elem.  Since the rest of our logic can already generate
			// the Go code for any value, we just wrap it in vom.RawBytesOf /
			// vdl.ValueOf to produce the final result.
			//
			// This may seem like a strange roundtrip, but results in less generator
			// and generated code.
			//
			// There's no need to convert the value to its native representation,
			// since it'll just be converted back in vom.RawBytesOf / vdl.ValueOf.
			if shouldUseVdlValueForAny(data.Package) {
				return data.Pkg("v.io/v23/vdl") + "ValueOf(" + typedConstWire(data, elem) + ")"
			} else {
				return data.Pkg("v.io/v23/vom") + "RawBytesOf(" + typedConstWire(data, elem) + ")"
			}
		}
		if shouldUseVdlValueForAny(data.Package) {
			return "(*" + data.Pkg("v.io/v23/vdl") + "Value)(nil)"
		} else {
			return "(*" + data.Pkg("v.io/v23/vom") + "RawBytes)(nil)"
		}
	case vdl.Optional:
		if elem := v.Elem(); elem != nil {
			return untypedConst(data, elem)
		}
		return "nil"
	case vdl.TypeObject:
		// We special-case Any and TypeObject, since they cannot be named by the
		// user, and are simple to return statically.
		switch v.TypeObject().Kind() {
		case vdl.Any:
			return data.Pkg("v.io/v23/vdl") + "AnyType"
		case vdl.TypeObject:
			return data.Pkg("v.io/v23/vdl") + "TypeObjectType"
		}
		// We need to generate a Go expression of type *vdl.Type that represents the
		// type.  Since the rest of our logic can already generate the Go code for
		// any value, we just wrap it in vdl.TypeOf to produce the final result.
		//
		// This may seem like a strange roundtrip, but results in less generator and
		// generated code.
		//
		// There's no need to convert the value to its native representation, since
		// it'll just be converted back in vdl.TypeOf.
		tt := v.TypeObject()
		typeOf := data.Pkg("v.io/v23/vdl") + "TypeOf((*" + typeGoWire(data, tt) + ")(nil))"
		if tt.CanBeOptional() && tt.Kind() != vdl.Optional {
			typeOf += ".Elem()"
		}
		return typeOf
	case vdl.Bool:
		return strconv.FormatBool(v.Bool())
	case vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case vdl.Float32, vdl.Float64:
		return formatFloat(v.Float(), k)
	case vdl.Complex64, vdl.Complex128:
		switch re, im := real(v.Complex()), imag(v.Complex()); {
		case im > 0:
			return formatFloat(re, k) + "+" + formatFloat(im, k) + "i"
		case im < 0:
			return formatFloat(re, k) + formatFloat(im, k) + "i"
		default:
			return formatFloat(re, k)
		}
	case vdl.String:
		return strconv.Quote(v.RawString())
	case vdl.Enum:
		return typestr + v.EnumLabel()
	case vdl.Array:
		if v.IsZero() && !t.ContainsKind(vdl.WalkInline, vdl.TypeObject, vdl.Union) {
			// We can't rely on the golang zero-value array if t contains inline
			// typeobject or union, since the golang zero-value for these types is
			// different from the vdl zero-value for these types.
			return "{}"
		}
		s := "{"
		for ix := 0; ix < v.Len(); ix++ {
			s += "\n" + untypedConst(data, v.Index(ix)) + ","
		}
		return s + "\n}"
	case vdl.List:
		if v.IsZero() {
			return "nil"
		}
		s := "{"
		for ix := 0; ix < v.Len(); ix++ {
			s += "\n" + untypedConst(data, v.Index(ix)) + ","
		}
		return s + "\n}"
	case vdl.Set, vdl.Map:
		if v.IsZero() {
			return "nil"
		}
		s := "{"
		for _, key := range vdl.SortValuesAsString(v.Keys()) {
			s += "\n" + untypedConst(data, key)
			if k == vdl.Set {
				s += ": struct{}{},"
			} else {
				s += ": " + untypedConst(data, v.MapIndex(key)) + ","
			}
		}
		return s + "\n}"
	case vdl.Struct:
		s := "{"
		hasFields := false
		for ix := 0; ix < t.NumField(); ix++ {
			vf := v.StructField(ix)
			if !vf.IsZero() || vf.Type().ContainsKind(vdl.WalkInline, vdl.TypeObject, vdl.Union) {
				// We can't rely on the golang zero-value for this field, even if it's a
				// vdl zero value, if the field contains inline typeobject or union,
				// since the golang zero-value for these types is different from the vdl
				// zero-value for these types.
				s += "\n" + t.Field(ix).Name + ": " + fieldConst(data, vf) + ","
				hasFields = true
			}
		}
		if hasFields {
			s += "\n"
		}
		return s + "}"
	case vdl.Union:
		ix, vf := v.UnionField()
		var inner string
		if !vf.IsZero() || vf.Type().ContainsKind(vdl.WalkInline, vdl.TypeObject, vdl.Union) {
			// We can't rely on the golang zero-value array if t contains inline
			// typeobject or union, since the golang zero-value for these types is
			// different from the vdl zero-value for these types.
			inner = fieldConst(data, vf)
		}
		return typestr + t.Field(ix).Name + "{" + inner + "}"
	default:
		data.Env.Errors.Errorf("%s: %v untypedConstWire not implemented for %v %v", data.Package.Name, t, k)
		return "INVALID"
	}
}

// fieldConst deals with a quirk regarding Go composite literals.  Go allows us
// to elide the type from composite literal Y when the type is implied;
// basically when Y is contained in another composite literal X.  However it
// requires the type for Y when X is a struct and we're filling in its fields.
//
// Thus fieldConst is called when filling in struct and union fields, and
// ensures typedConst is only called when the field itself will result in
// another Go composite literal.
func fieldConst(data *goData, v *vdl.Value) string {
	switch v.Kind() {
	case vdl.Array, vdl.List, vdl.Set, vdl.Map, vdl.Struct, vdl.Optional:
		return typedConst(data, v)
	}
	// Union is not in the list above.  It *is* generated as a Go composite
	// literal, but untypedConst already has the type of the concrete union struct
	// attached to it, and we don't need to convert this to the union interface
	// type, since the concrete union struct is assignable to the union interface.
	return untypedConst(data, v)
}

func formatFloat(x float64, kind vdl.Kind) string {
	var bitSize int
	switch kind {
	case vdl.Float32, vdl.Complex64:
		bitSize = 32
	case vdl.Float64, vdl.Complex128:
		bitSize = 64
	default:
		panic(fmt.Errorf("vdl: formatFloat unhandled kind: %v", kind))
	}
	return strconv.FormatFloat(x, 'g', -1, bitSize)
}

// nativeConst returns an inline function that returns the typed const for a
// native type.  Returns the empty string if v isn't a native type.
func nativeConst(data *goData, v *vdl.Value) string {
	def := data.Env.FindTypeDef(v.Type())
	if def == nil {
		return ""
	}
	pkg := def.File.Package
	native, ok := pkg.Config.Go.WireToNativeTypes[def.Name]
	if !ok {
		return ""
	}
	return fmt.Sprintf(`func() %[1]s {
	var native %[1]s
	if err := %[2]sConvert(&native, %[3]s); err != nil {
		panic(err)
	}
	return native
}()`, nativeIdent(data, native, pkg), data.Pkg("v.io/v23/vdl"), typedConstWire(data, v))
}

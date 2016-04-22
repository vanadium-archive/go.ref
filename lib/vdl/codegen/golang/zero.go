// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"fmt"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
)

// goZeroValueWire returns the Go expression that creates a zero value of wire
// type tt.
func goZeroValueWire(data *goData, tt *vdl.Type) string {
	// TODO(toddw): For native types, allow the user to tell us whether the Go
	// zero value is a valid zero representation in vdl.config, and if so, just
	// use the Go zero value.  This will be much faster than performing the
	// conversion, e.g. for time.Time.
	return typedConstWire(data, vdl.ZeroValue(tt))
}

// defineIsZero returns the VDLIsZero method for the def type.
func defineIsZero(data *goData, def *compile.TypeDef) string {
	g := genIsZero{goData: data}
	return g.Gen(def)
}

type genIsZero struct {
	*goData
}

// isGoZeroValueUnique returns true iff the Go zero value of the wire type tt
// represents the VDL zero value, and is the *only* value that represents the
// VDL zero value.
func isGoZeroValueUnique(data *goData, tt *vdl.Type) bool {
	// Not unique if tt contains types where there is more than one VDL zero value
	// representation:
	//   Any:            nil, or VDLIsZero on vdl.Value/vom.RawBytes
	//   TypeObject:     nil, or AnyType
	//   Union:          nil, or zero value of field 0
	//   List, Set, Map: nil, or empty
	if tt.ContainsKind(vdl.WalkInline, vdl.Any, vdl.TypeObject, vdl.Union, vdl.List, vdl.Set, vdl.Map) {
		return false
	}
	// Not unique if tt contains inline native subtypes, since native types can
	// choose to represent VDL zero values however they want.  This doesn't apply
	// if the type itself is native, but has no inline native subtypes, since
	// we're only considering the wire type form of tt.
	if containsInlineNativeSubTypes(data, tt) {
		return false
	}
	return true
}

func containsInlineNativeSubTypes(data *goData, tt *vdl.Type) bool {
	// The walk early-exits if the visitor functor returns false.  We want the
	// early-exit when we detect the first native type, so we use false to mean
	// that we've seen a native type, and true if we haven't.
	return !tt.Walk(vdl.WalkInline, func(visit *vdl.Type) bool {
		// We don't want the native check to fire for tt itself, so we return true
		// when we visit tt, meaning we haven't detected a native type yet.
		return visit == tt || !isNativeType(data.Env, visit)
	})
}

func (g *genIsZero) Gen(def *compile.TypeDef) string {
	// Special-case array and struct to check each elem or field for zero.
	// Special-case union to generate methods for each concrete union struct.
	//
	// Types that have a unique Go zero value can bypass these special-cases, and
	// perform a direct comparison against that zero value.  Note that def.Type
	// always represents a wire type here, since we're generating the VDLIsZero
	// method on the wire type.
	if !isGoZeroValueUnique(g.goData, def.Type) {
		switch def.Type.Kind() {
		case vdl.Array:
			return g.genArrayDef(def)
		case vdl.Struct:
			return g.genStructDef(def)
		case vdl.Union:
			return g.genUnionDef(def)
		}
	}
	return g.genDef(def)
}

func (g *genIsZero) genDef(def *compile.TypeDef) string {
	tt, arg := def.Type, namedArg{"x", false}
	setup, expr := g.ExprWire(eqZero, tt, arg, "", true, vdl.BoolType)
	if tt.Kind() == vdl.Bool {
		// Special-case named bool types, since we'll get an expression like "x",
		// but we need an explicit conversion since the type of x is the named bool
		// type, not the built-in bool type.
		expr = "bool(" + expr + ")"
	}
	return fmt.Sprintf(`
func (x %[1]s) VDLIsZero() (bool, error) {%[2]s
	return %[3]s, nil
}
`, def.Name, setup, expr)
}

func (g *genIsZero) genArrayDef(def *compile.TypeDef) string {
	tt := def.Type
	elemArg := typedArg("elem", tt.Elem())
	setup, expr, _ := g.Expr(neZero, tt.Elem(), elemArg, "", false, vdl.BoolType)
	return fmt.Sprintf(`
func (x %[1]s) VDLIsZero() (bool, error) {
	for _, elem := range x {%[2]s
		if %[3]s {
			return false, nil
		}
	}
	return true, nil
}
`, def.Name, setup, expr)
}

func (g *genIsZero) genStructDef(def *compile.TypeDef) string {
	tt, arg := def.Type, namedArg{"x", false}
	s := fmt.Sprintf(`
func (x %[1]s) VDLIsZero() (bool, error) {`, def.Name)
	for ix := 0; ix < tt.NumField(); ix++ {
		field := tt.Field(ix)
		setup, expr, _ := g.Expr(neZero, field.Type, arg.Field(field), field.Name, false, vdl.BoolType)
		s += fmt.Sprintf(`%[1]s
	if %[2]s {
		return false, nil
	}`, setup, expr)
	}
	s += `
	return true, nil
}
`
	return s
}

func (g *genIsZero) genUnionDef(def *compile.TypeDef) string {
	// The 0th field needs a real zero check.
	tt := def.Type
	field0 := tt.Field(0)
	fieldArg := typedArg("x.Value", field0.Type)
	setup, expr, _ := g.Expr(eqZero, field0.Type, fieldArg, "", true, vdl.BoolType)
	s := fmt.Sprintf(`
func (x %[1]s%[2]s) VDLIsZero() (bool, error) {%[3]s
	return %[4]s, nil
}
`, def.Name, field0.Name, setup, expr)
	// All other fields simply return false.
	for ix := 1; ix < tt.NumField(); ix++ {
		s += fmt.Sprintf(`
func (x %[1]s%[2]s) VDLIsZero() (bool, error) {
	return false, nil
}
`, def.Name, tt.Field(ix).Name)
	}
	return s
}

// returnErr creates the return statement when an error occurs.  Zero values of
// the given returnTypes are included before the final "err" variable.
func (g *genIsZero) returnErr(returnTypes []*vdl.Type) string {
	s := "return "
	for _, tt := range returnTypes {
		s += goZeroValueWire(g.goData, tt) + ", "
	}
	return s + "err"
}

type zeroExpr int

const (
	eqZero zeroExpr = iota // Generate equals-zero expression
	neZero                 // Generate not-equals-zero expression
)

// Expr generates the Go code to check whether the arg, which has type tt, is
// equal or not equal to zero.  The tmp string is appended to temporary variable
// names to make them unique.  The returnTypes specify the output types of the
// enclosing function, used to generating return statements when an error
// occurs.
//
// The returned setup and expr are meant to be used in the following fashion in
// generated code:
//
//   func foo() {
//     <setup>
//     if <expr> {
//       ...
//     }
//
// I.e. setup is a possibly-empty block of Go statements, while expr is a
// boolean Go expression that evalutes whether arg is zero or non-zero.
//
// If the pretty flag is true, we return a prettier form of expr, with the
// downside that expr is not valid in all contexts.  Examples:
//
//   // Pretty expr is:     "x == Foo{}"
//   // Non-pretty expr is: "x == (Foo{})"
//
//   // Pretty expr is not valid here, must use non-pretty.
//   if x == (Foo{}) {
//     ...
//   }
//
//   // Pretty expr is valid here.
//   return x == Foo{}
//
// The problem with using the pretty expr in the first example is that a parsing
// failure will occur in the generated Go code.
//
// The returned wireArg is valid iff a native conversion was performed, and the
// wireArg holds the converted wire value.
func (g *genIsZero) Expr(ze zeroExpr, tt *vdl.Type, arg namedArg, tmp string, pretty bool, returnTypes ...*vdl.Type) (setup, expr string, wireArg namedArg) {
	if isNativeType(g.Env, tt) {
		// TODO(toddw): Allow the user to configure an IsZero function or VDLIsZero
		// method in vdl.config, and call it directly.  That's faster for time.Time.
		wireArg = typedArg("wire"+tmp, tt)
		nativeSetup := fmt.Sprintf(`
	var %[1]s %[2]s
	if err := %[2]sFromNative(&%[1]s, %[3]s); err != nil {
		%[4]s
	}`, wireArg.Name, typeGoWire(g.goData, tt), arg.Ref(), g.returnErr(returnTypes))
		wireSetup, wireExpr := g.ExprWire(ze, tt, wireArg, tmp, pretty, returnTypes...)
		return nativeSetup + wireSetup, wireExpr, wireArg
	}
	wireSetup, wireExpr := g.ExprWire(ze, tt, arg, tmp, pretty, returnTypes...)
	return wireSetup, wireExpr, namedArg{}
}

// ExprWire is like Expr, but generates code for the wire type tt.
func (g *genIsZero) ExprWire(ze zeroExpr, tt *vdl.Type, arg namedArg, tmp string, pretty bool, returnTypes ...*vdl.Type) (setup, expr string) {
	opNot, eq, cond, ref := "", "==", "||", arg.Ref()
	if ze == neZero {
		opNot, eq, cond = "!", "!=", "&&"
	}
	// Handle everything other than Array and Struct.
	switch tt.Kind() {
	case vdl.Bool:
		if ze == neZero {
			return "", ref
		}
		return "", "!" + ref // false is zero, while true is non-zero
	case vdl.String:
		return "", ref + eq + `""`
	case vdl.Byte, vdl.Uint16, vdl.Uint32, vdl.Uint64, vdl.Int8, vdl.Int16, vdl.Int32, vdl.Int64, vdl.Float32, vdl.Float64:
		return "", ref + eq + "0"
	case vdl.Enum:
		return "", ref + eq + typeGoWire(g.goData, tt) + tt.EnumLabel(0)
	case vdl.TypeObject:
		return "", ref + eq + "nil" + cond + ref + eq + g.Pkg("v.io/v23/vdl") + "AnyType"
	case vdl.List, vdl.Set, vdl.Map:
		return "", "len(" + ref + ")" + eq + "0"
	case vdl.Optional:
		return "", arg.Name + eq + "nil"
	case vdl.Union, vdl.Any:
		// Union is always named, and Any is either *vdl.Value or *vom.RawBytes, so
		// we call VDLIsZero directly.  A slight complication is the fact that all
		// of these might be nil, which we need to protect against before making the
		// VDLIsZero call.
		zeroVar := "isZero" + tmp
		wireSetup := fmt.Sprintf(`
	var %[1]s bool
	if %[2]s != nil {
		var err error
		if %[1]s, err = %[2]s.VDLIsZero(); err != nil {
			%[3]s
		}
	}`, zeroVar, arg.Name, g.returnErr(returnTypes))
		return wireSetup, arg.Name + eq + "nil" + cond + opNot + zeroVar
	}
	// Only Array and Struct are left.
	//
	// If there is a unique Go zero value, we generate a fastpath that simply
	// compares against that value.  Note that tt always represents a wire type
	// here, since native types were handled in Expr.
	if isGoZeroValueUnique(g.goData, tt) {
		zeroVal := goZeroValueWire(g.goData, tt)
		if !pretty {
			zeroVal = "(" + zeroVal + ")"
		}
		return "", ref + eq + zeroVal
	}
	// Otherwise we call VDLIsZero directly.  This takes advantage of the fact
	// that Array and Struct are always named, so will always have a VDLIsZero
	// method defined.
	zeroVar := "isZero" + tmp
	wireSetup := fmt.Sprintf(`
	%[1]s, err := %[2]s.VDLIsZero()
	if err != nil {
		%[3]s
	}`, zeroVar, arg.Name, g.returnErr(returnTypes))
	return wireSetup, opNot + zeroVar
}

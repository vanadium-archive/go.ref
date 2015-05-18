// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package java

import (
	"bytes"
	"fmt"
	"log"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
)

const complexTmpl = header + `
// Source: {{.Source}}
package {{.PackagePath}};

{{ .Doc }}
@io.v.v23.vdl.GeneratedFromVdl(name = "{{.VdlTypeName}}")
{{ .AccessModifier }} class {{.Name}} extends {{.VdlComplex}} {
    private static final long serialVersionUID = 1L;

    /**
     * Vdl type for {@link {{.Name}}}.
     */
    public static final io.v.v23.vdl.VdlType VDL_TYPE =
            io.v.v23.vdl.Types.getVdlTypeFromReflect({{.Name}}.class);

    /**
     * Creates a new instance of {@link {{.Name}}} with the given real and imaginary parts.
     *
     * @param real real part
     * @param imag imaginary part
     */
    public {{.Name}}({{.ValueType}} real, {{.ValueType}} imag) {
        super(VDL_TYPE, real, imag);
    }

    /**
     * Creates a new instance of {@link {{.Name}}} with the given real part and a zero imaginary
     * part.
     *
     * @param real real part
     */
    public {{.Name}}({{.ValueType}} real) {
        this(real, 0);
    }

    /**
     * Creates a new zero-value instance of {@link {{.Name}}}.
     */
    public {{.Name}}() {
        this(0, 0);
    }
}
`

// genJavaComplexFile generates the Java class file for the provided user-defined VDL complex type.
func genJavaComplexFile(tdef *compile.TypeDef, env *compile.Env) JavaFileInfo {
	var ValueType string
	switch kind := tdef.Type.Kind(); kind {
	case vdl.Complex64:
		ValueType = "float"
	case vdl.Complex128:
		ValueType = "double"
	default:
		panic(fmt.Errorf("val: unhandled kind: %v", kind))
	}
	name, access := javaTypeName(tdef, env)
	data := struct {
		AccessModifier string
		Doc            string
		FileDoc        string
		Name           string
		PackagePath    string
		Source         string
		ValueType      string
		VdlComplex     string
		VdlTypeName    string
		VdlTypeString  string
	}{
		AccessModifier: access,
		Doc:            javaDoc(tdef.Doc, tdef.DocSuffix),
		FileDoc:        tdef.File.Package.FileDoc,
		Name:           name,
		PackagePath:    javaPath(javaGenPkgPath(tdef.File.Package.GenPath)),
		Source:         tdef.File.BaseName,
		ValueType:      ValueType,
		VdlComplex:     javaVdlPrimitiveType(tdef.Type.Kind()),
		VdlTypeName:    tdef.Type.Name(),
		VdlTypeString:  tdef.Type.String(),
	}
	var buf bytes.Buffer
	err := parseTmpl("complex", complexTmpl).Execute(&buf, data)
	if err != nil {
		log.Fatalf("vdl: couldn't execute VDL complex template: %v", err)
	}
	return JavaFileInfo{
		Name: name + ".java",
		Data: buf.Bytes(),
	}
}

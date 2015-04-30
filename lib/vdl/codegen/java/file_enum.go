// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package java

import (
	"bytes"
	"log"

	"v.io/x/ref/lib/vdl/compile"
	"v.io/x/ref/lib/vdl/vdlutil"
)

const enumTmpl = header + `
// Source: {{.Source}}
package {{.PackagePath}};

/**
 * type {{.Name}} {{.VdlTypeString}} {{.Doc}}
 **/
@io.v.v23.vdl.GeneratedFromVdl(name = "{{.VdlTypeName}}")
{{ .AccessModifier }} class {{.Name}} extends io.v.v23.vdl.VdlEnum {
    {{ range $index, $label := .EnumLabels }}
        @io.v.v23.vdl.GeneratedFromVdl(name = "{{$label}}", index = {{$index}})
        public static final {{$.Name}} {{$label}};
    {{ end }}

    public static final io.v.v23.vdl.VdlType VDL_TYPE =
            io.v.v23.vdl.Types.getVdlTypeFromReflect({{.Name}}.class);

    static {
        {{ range $label := .EnumLabels }}
            {{$label}} = new {{$.Name}}("{{$label}}");
        {{ end }}
    }

    private {{.Name}}(String name) {
        super(VDL_TYPE, name);
    }

    public static {{.Name}} valueOf(String name) {
        {{ range $label := .EnumLabels }}
            if ("{{$label}}".equals(name)) {
                return {{$label}};
            }
        {{ end }}
        throw new java.lang.IllegalArgumentException();
    }
}
`

// genJavaEnumFile generates the Java class file for the provided user-defined enum type.
func genJavaEnumFile(tdef *compile.TypeDef, env *compile.Env) JavaFileInfo {
	labels := make([]string, tdef.Type.NumEnumLabel())
	for i := 0; i < tdef.Type.NumEnumLabel(); i++ {
		labels[i] = tdef.Type.EnumLabel(i)
	}
	javaTypeName := vdlutil.FirstRuneToUpper(tdef.Name)
	data := struct {
		FileDoc        string
		AccessModifier string
		EnumLabels     []string
		Doc            string
		Name           string
		PackagePath    string
		Source         string
		VdlTypeName    string
		VdlTypeString  string
	}{
		FileDoc:        tdef.File.Package.FileDoc,
		AccessModifier: accessModifierForName(tdef.Name),
		EnumLabels:     labels,
		Doc:            javaDocInComment(tdef.Doc),
		Name:           javaTypeName,
		PackagePath:    javaPath(javaGenPkgPath(tdef.File.Package.GenPath)),
		Source:         tdef.File.BaseName,
		VdlTypeName:    tdef.Type.Name(),
		VdlTypeString:  tdef.Type.String(),
	}
	var buf bytes.Buffer
	err := parseTmpl("enum", enumTmpl).Execute(&buf, data)
	if err != nil {
		log.Fatalf("vdl: couldn't execute enum template: %v", err)
	}
	return JavaFileInfo{
		Name: javaTypeName + ".java",
		Data: buf.Bytes(),
	}
}

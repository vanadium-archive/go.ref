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

const listTmpl = header + `
// Source: {{.SourceFile}}
package {{.Package}};

/**
 * type {{.Name}} {{.VdlTypeString}} {{.Doc}}
 **/
@io.v.v23.vdl.GeneratedFromVdl(name = "{{.VdlTypeName}}")
{{ .AccessModifier }} class {{.Name}} extends io.v.v23.vdl.VdlList<{{.ElemType}}> {
	private static final long serialVersionUID = 1L;

    public static final io.v.v23.vdl.VdlType VDL_TYPE =
            io.v.v23.vdl.Types.getVdlTypeFromReflect({{.Name}}.class);

    public {{.Name}}(java.util.List<{{.ElemType}}> impl) {
        super(VDL_TYPE, impl);
    }

    public {{.Name}}() {
        this(new java.util.ArrayList<{{.ElemType}}>());
    }
}
`

// genJavaListFile generates the Java class file for the provided named list type.
func genJavaListFile(tdef *compile.TypeDef, env *compile.Env) JavaFileInfo {
	javaTypeName := vdlutil.FirstRuneToUpper(tdef.Name)
	data := struct {
		FileDoc        string
		AccessModifier string
		Doc            string
		ElemType       string
		Name           string
		Package        string
		SourceFile     string
		VdlTypeName    string
		VdlTypeString  string
	}{
		FileDoc:        tdef.File.Package.FileDoc,
		AccessModifier: accessModifierForName(tdef.Name),
		Doc:            javaDocInComment(tdef.Doc),
		ElemType:       javaType(tdef.Type.Elem(), true, env),
		Name:           javaTypeName,
		Package:        javaPath(javaGenPkgPath(tdef.File.Package.GenPath)),
		SourceFile:     tdef.File.BaseName,
		VdlTypeName:    tdef.Type.Name(),
		VdlTypeString:  tdef.Type.String(),
	}
	var buf bytes.Buffer
	err := parseTmpl("list", listTmpl).Execute(&buf, data)
	if err != nil {
		log.Fatalf("vdl: couldn't execute list template: %v", err)
	}
	return JavaFileInfo{
		Name: javaTypeName + ".java",
		Data: buf.Bytes(),
	}
}

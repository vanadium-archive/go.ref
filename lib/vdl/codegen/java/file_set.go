// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package java

import (
	"bytes"
	"log"

	"v.io/x/ref/lib/vdl/compile"
)

const setTmpl = header + `
// Source: {{.SourceFile}}

package {{.Package}};

/**
 * {{.Name}} {{.VdlTypeString}} {{.Doc}}
 **/
@io.v.v23.vdl.GeneratedFromVdl(name = "{{.VdlTypeName}}")
{{ .AccessModifier }} class {{.Name}} extends io.v.v23.vdl.VdlSet<{{.KeyType}}> {
	private static final long serialVersionUID = 1L;

    public static final io.v.v23.vdl.VdlType VDL_TYPE =
            io.v.v23.vdl.Types.getVdlTypeFromReflect({{.Name}}.class);

    public {{.Name}}(java.util.Set<{{.KeyType}}> impl) {
        super(VDL_TYPE, impl);
    }

    public {{.Name}}() {
        this(new java.util.HashSet<{{.KeyType}}>());
    }
}
`

// genJavaSetFile generates the Java class file for the provided named set type.
func genJavaSetFile(tdef *compile.TypeDef, env *compile.Env) JavaFileInfo {
	name, access := javaTypeName(tdef, env)
	data := struct {
		FileDoc        string
		AccessModifier string
		Doc            string
		KeyType        string
		Name           string
		Package        string
		SourceFile     string
		VdlTypeName    string
		VdlTypeString  string
	}{
		FileDoc:        tdef.File.Package.FileDoc,
		AccessModifier: access,
		Doc:            javaDocInComment(tdef.Doc),
		KeyType:        javaType(tdef.Type.Key(), true, env),
		Name:           name,
		Package:        javaPath(javaGenPkgPath(tdef.File.Package.GenPath)),
		SourceFile:     tdef.File.BaseName,
		VdlTypeName:    tdef.Type.Name(),
		VdlTypeString:  tdef.Type.String(),
	}
	var buf bytes.Buffer
	err := parseTmpl("set", setTmpl).Execute(&buf, data)
	if err != nil {
		log.Fatalf("vdl: couldn't execute set template: %v", err)
	}
	return JavaFileInfo{
		Name: name + ".java",
		Data: buf.Bytes(),
	}
}

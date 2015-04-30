// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package java

import (
	"bytes"
	"log"

	"v.io/v23/vdl"
	"v.io/x/ref/lib/vdl/compile"
	"v.io/x/ref/lib/vdl/vdlutil"
)

const structTmpl = header + `
// Source: {{.Source}}
package {{.PackagePath}};

/**
 * type {{.Name}} {{.VdlTypeString}} {{.Doc}}
 **/
@io.v.v23.vdl.GeneratedFromVdl(name = "{{.VdlTypeName}}")
{{ .AccessModifier }} class {{.Name}} extends io.v.v23.vdl.AbstractVdlStruct {
    private static final long serialVersionUID = 1L;

    {{/* Field declarations */}}
    {{ range $index, $field := .Fields }}
      @io.v.v23.vdl.GeneratedFromVdl(name = "{{$field.Name}}", index = {{$index}})
      private {{$field.Type}} {{$field.LowercaseName}};
    {{ end }}

    public static final io.v.v23.vdl.VdlType VDL_TYPE =
            io.v.v23.vdl.Types.getVdlTypeFromReflect({{.Name}}.class);

    {{/* Constructors */}}
    public {{.Name}}() {
        super(VDL_TYPE);
        {{ range $field := .Fields }}
            this.{{$field.LowercaseName}} = {{$field.ZeroValue}};
        {{ end }}
    }

    {{ if .FieldsAsArgs }}
    public {{.Name}}({{ .FieldsAsArgs }}) {
        super(VDL_TYPE);
        {{ range $field := .Fields }}
            this.{{$field.LowercaseName}} = {{$field.LowercaseName}};
        {{ end }}
    }
    {{ end }}

    {{/* Getters and setters */}}
    {{ range $field := .Fields }}
    {{ $field.AccessModifier }} {{$field.Type}} get{{$field.Name}}() {
        return this.{{$field.LowercaseName}};
    }

    {{ $field.AccessModifier }} void set{{$field.Name}}({{$field.Type}} {{$field.LowercaseName}}) {
        this.{{$field.LowercaseName}} = {{$field.LowercaseName}};
    }
    {{ end }}

    @Override
    public boolean equals(java.lang.Object obj) {
        if (this == obj) return true;
        if (obj == null) return false;
        if (this.getClass() != obj.getClass()) return false;
	{{ if gt (len .Fields) 0 }} final {{.Name}} other = ({{.Name}})obj; {{ end }}

        {{ range $field := .Fields }}
        {{ if .IsArray }}
        if (!java.util.Arrays.equals(this.{{$field.LowercaseName}}, other.{{$field.LowercaseName}})) {
            return false;
        }
        {{ else }}
        {{ if .IsClass }}
        if (this.{{$field.LowercaseName}} == null) {
            if (other.{{$field.LowercaseName}} != null) {
                return false;
            }
        } else if (!this.{{$field.LowercaseName}}.equals(other.{{$field.LowercaseName}})) {
            return false;
        }
        {{ else }}
        if (this.{{$field.LowercaseName}} != other.{{$field.LowercaseName}}) {
            return false;
        }
        {{ end }} {{/* if is class */}}
        {{ end }} {{/* if is array */}}
        {{ end }} {{/* range over fields */}}
        return true;
    }

    @Override
    public int hashCode() {
        int result = 1;
        final int prime = 31;
        {{ range $field := .Fields }}
        result = prime * result + {{$field.HashcodeComputation}};
        {{ end }}
        return result;
    }

    @Override
    public java.lang.String toString() {
        String result = "{";
        {{ range $index, $field := .Fields }}
            {{ if gt $index 0 }}
                result += ", ";
            {{ end }}
            {{ if .IsArray }}
                result += "{{$field.LowercaseName}}:" + java.util.Arrays.toString({{$field.LowercaseName}});
            {{ else }}
            result += "{{$field.LowercaseName}}:" + {{$field.LowercaseName}};
            {{ end}} {{/* if is array */}}
        {{ end }} {{/* range over fields */}}
        return result + "}";
    }
}`

type structDefinitionField struct {
	AccessModifier      string
	Class               string
	HashcodeComputation string
	IsClass             bool
	IsArray             bool
	LowercaseName       string
	Name                string
	Type                string
	ZeroValue           string
}

func javaFieldArgStr(structType *vdl.Type, env *compile.Env) string {
	var buf bytes.Buffer
	for i := 0; i < structType.NumField(); i++ {
		if i > 0 {
			buf.WriteString(", ")
		}
		fld := structType.Field(i)
		buf.WriteString("final ")
		buf.WriteString(javaType(fld.Type, false, env))
		buf.WriteString(" ")
		buf.WriteString(vdlutil.FirstRuneToLower(fld.Name))
	}
	return buf.String()
}

// genJavaStructFile generates the Java class file for the provided user-defined type.
func genJavaStructFile(tdef *compile.TypeDef, env *compile.Env) JavaFileInfo {
	fields := make([]structDefinitionField, tdef.Type.NumField())
	for i := 0; i < tdef.Type.NumField(); i++ {
		fld := tdef.Type.Field(i)
		fields[i] = structDefinitionField{
			AccessModifier:      accessModifierForName(fld.Name),
			Class:               javaType(fld.Type, true, env),
			HashcodeComputation: javaHashCode(vdlutil.FirstRuneToLower(fld.Name), fld.Type, env),
			IsClass:             isClass(fld.Type, env),
			IsArray:             isJavaNativeArray(fld.Type, env),
			LowercaseName:       vdlutil.FirstRuneToLower(fld.Name),
			Name:                fld.Name,
			Type:                javaType(fld.Type, false, env),
			ZeroValue:           javaZeroValue(fld.Type, env),
		}
	}

	javaTypeName := vdlutil.FirstRuneToUpper(tdef.Name)
	data := struct {
		FileDoc        string
		AccessModifier string
		Doc            string
		Fields         []structDefinitionField
		FieldsAsArgs   string
		Name           string
		PackagePath    string
		Source         string
		VdlTypeName    string
		VdlTypeString  string
	}{
		FileDoc:        tdef.File.Package.FileDoc,
		AccessModifier: accessModifierForName(tdef.Name),
		Doc:            javaDocInComment(tdef.Doc),
		Fields:         fields,
		FieldsAsArgs:   javaFieldArgStr(tdef.Type, env),
		Name:           javaTypeName,
		PackagePath:    javaPath(javaGenPkgPath(tdef.File.Package.GenPath)),
		Source:         tdef.File.BaseName,
		VdlTypeName:    tdef.Type.Name(),
		VdlTypeString:  tdef.Type.String(),
	}
	var buf bytes.Buffer
	err := parseTmpl("struct", structTmpl).Execute(&buf, data)
	if err != nil {
		log.Fatalf("vdl: couldn't execute struct template: %v", err)
	}
	return JavaFileInfo{
		Name: javaTypeName + ".java",
		Data: buf.Bytes(),
	}
}

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

const errorTmpl = header + `
// Source(s): {{ .Source }}
package {{ .PackagePath }};

/**
 * Errors defined in all VDL files in this package.
 */
public final class {{ .ClassName }} {
    {{ range $file := .Files }}

    /* The following errors originate in file: {{ $file.Name }} */
    {{/*Error Defs*/}}
    {{ range $error := $file.Errors }}
    {{ $error.Doc }}
    {{ $error.AccessModifier }} static final io.v.v23.verror.VException.IDAction {{ $error.Name }} = io.v.v23.verror.VException.register("{{ $error.ID }}", io.v.v23.verror.VException.ActionCode.{{ $error.ActionName }}, "{{ $error.EnglishFmt }}");
    {{ end }} {{/* range $file.Errors */}}

    {{ end }} {{/* range .Files */}}

    static {
        {{ range $file := .Files }}
        /* The following errors originate in file: {{ $file.Name }} */
        {{ range $error := $file.Errors }}
        {{ range $format := $error.Formats}}
        io.v.v23.i18n.Language.getDefaultCatalog().setWithBase("{{ $format.Lang }}", {{ $error.Name }}.getID(), "{{ $format.Fmt }}");
        {{ end }} {{/* range $error.Formats */}}
        {{ end }} {{/* range $file.Errors */}}
        {{ end }} {{/* range .Files */}}
    }

    {{ range $file := .Files }}
    /* The following error creator methods originate in file: {{ $file.Name }} */
    {{ range $error := $file.Errors }}
    /**
     * Creates an error with the {@link #{{ $error.Name }}} identifier.
     */
    {{ $error.AccessModifier }} static io.v.v23.verror.VException {{ $error.MethodName }}(io.v.v23.context.VContext _ctx{{ $error.MethodArgs}}) {
        java.lang.Object[] _params = new java.lang.Object[] { {{ $error.Params }} };
        java.lang.reflect.Type[] _paramTypes = new java.lang.reflect.Type[]{ {{ $error.ParamTypes }} };
        return new io.v.v23.verror.VException({{ $error.Name }}, _ctx, _paramTypes, _params);
    }
    {{ end }} {{/* range $file.Errors */}}
    {{ end }} {{/* range .Files */}}

    private {{ .ClassName }}() {}
}
`

type errorDef struct {
	AccessModifier string
	Doc            string
	Name           string
	ID             string
	ActionName     string
	EnglishFmt     string
	Formats        []errorFormat
	MethodName     string
	MethodArgs     string
	Params         string
	ParamTypes     string
}

type errorFormat struct {
	Lang string
	Fmt  string
}

type errorFile struct {
	Name   string
	Errors []errorDef
}

func shouldGenerateErrorFile(pkg *compile.Package) bool {
	for _, file := range pkg.Files {
		if len(file.ErrorDefs) > 0 {
			return true
		}
	}
	return false
}

// genJavaErrorFile generates the (single) Java file that contains error
// definitions from all the VDL files.
func genJavaErrorFile(pkg *compile.Package, env *compile.Env) *JavaFileInfo {
	if !shouldGenerateErrorFile(pkg) {
		return nil
	}

	className := "Errors"

	files := make([]errorFile, len(pkg.Files))
	for i, file := range pkg.Files {
		errors := make([]errorDef, len(file.ErrorDefs))
		for j, err := range file.ErrorDefs {
			formats := make([]errorFormat, len(err.Formats))
			for k, format := range err.Formats {
				formats[k].Lang = string(format.Lang)
				formats[k].Fmt = format.Fmt
			}
			errors[j].AccessModifier = accessModifierForName(err.Name)
			errors[j].Doc = javaDoc(err.Doc, err.DocSuffix)
			errors[j].Name = vdlutil.ToConstCase(err.Name)
			errors[j].ID = err.ID
			errors[j].ActionName = vdlutil.ToConstCase(err.RetryCode.String())
			errors[j].EnglishFmt = err.English
			errors[j].Formats = formats
			errors[j].MethodName = "new" + vdlutil.FirstRuneToUpper(err.Name)
			errors[j].MethodArgs = javaDeclarationArgStr(err.Params, env, true)
			errors[j].Params = javaCallingArgStr(err.Params, false)
			errors[j].ParamTypes = javaCallingArgTypeStr(err.Params, env)
		}
		files[i].Name = file.BaseName
		files[i].Errors = errors
	}

	data := struct {
		ClassName   string
		FileDoc     string
		Source      string
		PackagePath string
		Files       []errorFile
	}{
		ClassName:   className,
		FileDoc:     pkg.FileDoc,
		Source:      javaFileNames(pkg.Files),
		PackagePath: javaPath(javaGenPkgPath(pkg.GenPath)),
		Files:       files,
	}
	var buf bytes.Buffer
	err := parseTmpl("error", errorTmpl).Execute(&buf, data)
	if err != nil {
		log.Fatalf("vdl: couldn't execute error template: %v", err)
	}
	return &JavaFileInfo{
		Name: className + ".java",
		Data: buf.Bytes(),
	}
}

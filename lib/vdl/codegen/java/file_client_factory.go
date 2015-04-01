// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package java

import (
	"bytes"
	"log"
	"path"

	"v.io/x/ref/lib/vdl/compile"
	"v.io/x/ref/lib/vdl/vdlutil"
)

const clientFactoryTmpl = header + `
// Source(s):  {{ .Sources }}
package {{ .PackagePath }};

/* Factory for binding to {{ .ServiceName }}Client interfaces. */
{{.AccessModifier}} final class {{ .ServiceName }}ClientFactory {
    public static {{ .ServiceName }}Client bind(final java.lang.String name) {
        return bind(name, null);
    }
    public static {{ .ServiceName }}Client bind(final java.lang.String name, final io.v.v23.Options vOpts) {
        io.v.v23.rpc.Client client = null;
        if (vOpts != null && vOpts.get(io.v.v23.OptionDefs.CLIENT) != null) {
            client = vOpts.get(io.v.v23.OptionDefs.CLIENT, io.v.v23.rpc.Client.class);
        }
        return new {{ .StubName }}(client, name);
    }
}
`

// genJavaClientFactoryFile generates the Java file containing client bindings for
// all interfaces in the provided package.
func genJavaClientFactoryFile(iface *compile.Interface, env *compile.Env) JavaFileInfo {
	javaServiceName := vdlutil.FirstRuneToUpper(iface.Name)
	data := struct {
		FileDoc        string
		AccessModifier string
		Sources        string
		ServiceName    string
		PackagePath    string
		StubName       string
	}{
		FileDoc:        iface.File.Package.FileDoc,
		AccessModifier: accessModifierForName(iface.Name),
		Sources:        iface.File.BaseName,
		ServiceName:    javaServiceName,
		PackagePath:    javaPath(javaGenPkgPath(iface.File.Package.GenPath)),
		StubName:       javaPath(javaGenPkgPath(path.Join(iface.File.Package.GenPath, iface.Name+"ClientStub"))),
	}
	var buf bytes.Buffer
	err := parseTmpl("client factory", clientFactoryTmpl).Execute(&buf, data)
	if err != nil {
		log.Fatalf("vdl: couldn't execute client template: %v", err)
	}
	return JavaFileInfo{
		Name: javaServiceName + "ClientFactory.java",
		Data: buf.Bytes(),
	}
}

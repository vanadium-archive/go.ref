// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package java

import (
	"bytes"
	"log"
	"path"
	"strings"

	"v.io/x/ref/lib/vdl/compile"
	"v.io/x/ref/lib/vdl/vdlutil"
)

const serverWrapperTmpl = header + `
// Source(s):  {{ .Source }}
package {{ .PackagePath }};

{{ .AccessModifier }} final class {{ .ServiceName }}ServerWrapper {

    private final {{ .FullServiceName }}Server server;

{{/* Define fields to hold each of the embedded server wrappers*/}}
{{ range $embed := .Embeds }}
    {{/* e.g. private final com.somepackage.gen_impl.ArithStub stubArith; */}}
    private final {{ $embed.WrapperClassName }} {{ $embed.LocalWrapperVarName }};
    {{ end }}

    public {{ .ServiceName }}ServerWrapper(final {{ .FullServiceName }}Server server) {
        this.server = server;
        {{/* Initialize the embeded server wrappers */}}
        {{ range $embed := .Embeds }}
        this.{{ $embed.LocalWrapperVarName }} = new {{ $embed.WrapperClassName }}(server);
        {{ end }}
    }

    /**
     * Returns a description of this server.
     */
    public io.v.v23.vdlroot.signature.Interface signature() {
        java.util.List<io.v.v23.vdlroot.signature.Embed> embeds = new java.util.ArrayList<io.v.v23.vdlroot.signature.Embed>();
        java.util.List<io.v.v23.vdlroot.signature.Method> methods = new java.util.ArrayList<io.v.v23.vdlroot.signature.Method>();
        {{ range $method := .Methods }}
        {
            java.util.List<io.v.v23.vdlroot.signature.Arg> inArgs = new java.util.ArrayList<io.v.v23.vdlroot.signature.Arg>();
            {{ range $arg := $method.CallingArgTypes }}
            inArgs.add(new io.v.v23.vdlroot.signature.Arg("", "", new io.v.v23.vdl.VdlTypeObject({{ $arg }})));
            {{ end }}
            java.util.List<io.v.v23.vdlroot.signature.Arg> outArgs = new java.util.ArrayList<io.v.v23.vdlroot.signature.Arg>();
            {{ range $arg := $method.RetJavaTypes }}
            outArgs.add(new io.v.v23.vdlroot.signature.Arg("", "", new io.v.v23.vdl.VdlTypeObject({{ $arg }})));
            {{ end }}
            java.util.List<io.v.v23.vdl.VdlAny> tags = new java.util.ArrayList<io.v.v23.vdl.VdlAny>();
            {{ range $tag := .Tags }}
            tags.add(new io.v.v23.vdl.VdlAny(io.v.v23.vdl.VdlValue.valueOf({{ $tag.Value }}, {{ $tag.Type }})));
            {{ end }}
            methods.add(new io.v.v23.vdlroot.signature.Method(
                "{{ $method.Name }}",
                "{{ $method.Doc }}",
                inArgs,
                outArgs,
                null,
                null,
                tags));
        }
        {{ end }}

        return new io.v.v23.vdlroot.signature.Interface("{{ .ServiceName }}", "{{ .PackagePath }}", "{{ .Doc }}", embeds, methods);
    }

    /**
     * Returns all tags associated with the provided method or null if the method isn't implemented
     * by this server.
     */
    @SuppressWarnings("unused")
    public io.v.v23.vdl.VdlValue[] getMethodTags(final java.lang.String method) throws io.v.v23.verror.VException {
        {{ range $methodName, $tags := .MethodTags }}
        if ("{{ $methodName }}".equals(method)) {
            try {
                return new io.v.v23.vdl.VdlValue[] {
                    {{ range $tag := $tags }} io.v.v23.vdl.VdlValue.valueOf({{ $tag.Value }}, {{ $tag.Type }}), {{ end }}
                };
            } catch (IllegalArgumentException e) {
                throw new io.v.v23.verror.VException(String.format("Couldn't get tags for method \"{{ $methodName }}\": %s", e.getMessage()));
            }
        }
        {{ end }}
        {{ range $embed := .Embeds }}
        {
            final io.v.v23.vdl.VdlValue[] tags = this.{{ $embed.LocalWrapperVarName }}.getMethodTags(method);
            if (tags != null) {
                return tags;
            }
        }
        {{ end }}
        return null;  // method not found
    }

     {{/* Iterate over methods defined directly in the body of this server */}}
    {{ range $method := .Methods }}
    {{ $method.AccessModifier }} {{ $method.RetType }} {{ $method.Name }}(final io.v.v23.context.VContext ctx, final io.v.v23.rpc.StreamServerCall call{{ $method.DeclarationArgs }}) throws io.v.v23.verror.VException {
        {{ if $method.IsStreaming }}
        final io.v.v23.vdl.Stream<{{ $method.SendType }}, {{ $method.RecvType }}> _stream = new io.v.v23.vdl.Stream<{{ $method.SendType }}, {{ $method.RecvType }}>() {
            @Override
            public void send({{ $method.SendType }} item) throws io.v.v23.verror.VException {
                final java.lang.reflect.Type type = new com.google.common.reflect.TypeToken< {{ $method.SendType }} >() {}.getType();
                call.send(item, type);
            }
            @Override
            public {{ $method.RecvType }} recv() throws java.io.EOFException, io.v.v23.verror.VException {
                final java.lang.reflect.Type type = new com.google.common.reflect.TypeToken< {{ $method.RecvType }} >() {}.getType();
                final java.lang.Object result = call.recv(type);
                try {
                    return ({{ $method.RecvType }})result;
                } catch (java.lang.ClassCastException e) {
                    throw new io.v.v23.verror.VException("Unexpected result type: " + result.getClass().getCanonicalName());
                }
            }
        };
        {{ end }} {{/* end if $method.IsStreaming */}}
        {{ if $method.Returns }} return {{ end }} this.server.{{ $method.Name }}(ctx, call {{ $method.CallingArgs }} {{ if $method.IsStreaming }} ,_stream {{ end }} );
    }
{{end}}

{{/* Iterate over methods from embeded servers and generate code to delegate the work */}}
{{ range $eMethod := .EmbedMethods }}
    {{ $eMethod.AccessModifier }} {{ $eMethod.RetType }} {{ $eMethod.Name }}(final io.v.v23.context.VContext ctx, final io.v.v23.rpc.StreamServerCall call{{ $eMethod.DeclarationArgs }}) throws io.v.v23.verror.VException {
        {{/* e.g. return this.stubArith.cosine(ctx, call, [args], options) */}}
        {{ if $eMethod.Returns }}return{{ end }}  this.{{ $eMethod.LocalWrapperVarName }}.{{ $eMethod.Name }}(ctx, call{{ $eMethod.CallingArgs }});
    }
{{ end }} {{/* end range .EmbedMethods */}}

}
`

type serverWrapperMethod struct {
	AccessModifier  string
	CallingArgs     string
	CallingArgTypes []string
	DeclarationArgs string
	IsStreaming     bool
	Name            string
	RecvType        string
	RetType         string
	RetJavaTypes    []string
	Returns         bool
	SendType        string
	Doc             string
	Tags            []methodTag
}

type serverWrapperEmbedMethod struct {
	AccessModifier      string
	CallingArgs         string
	DeclarationArgs     string
	LocalWrapperVarName string
	Name                string
	RetType             string
	Returns             bool
}

type serverWrapperEmbed struct {
	LocalWrapperVarName string
	WrapperClassName    string
}

type methodTag struct {
	Value string
	Type  string
}

// TODO(sjr): move this to somewhere in util_*.
func toJavaString(goString string) string {
	result := strings.Replace(goString, "\"", "\\\"", -1)
	result = strings.Replace(result, "\n", "\" + \n\"", -1)
	return result
}

func processServerWrapperMethod(iface *compile.Interface, method *compile.Method, env *compile.Env, tags []methodTag) serverWrapperMethod {
	callArgTypes := make([]string, len(method.InArgs))
	for i, arg := range method.InArgs {
		callArgTypes[i] = javaReflectType(arg.Type, env)
	}
	retArgTypes := make([]string, len(method.OutArgs))
	for i, arg := range method.OutArgs {
		retArgTypes[i] = javaReflectType(arg.Type, env)
	}
	return serverWrapperMethod{
		AccessModifier:  accessModifierForName(method.Name),
		CallingArgs:     javaCallingArgStr(method.InArgs, true),
		CallingArgTypes: callArgTypes,
		DeclarationArgs: javaDeclarationArgStr(method.InArgs, env, true),
		IsStreaming:     isStreamingMethod(method),
		Name:            vdlutil.FirstRuneToLower(method.Name),
		RecvType:        javaType(method.InStream, true, env),
		RetType:         serverInterfaceOutArg(iface, method, env),
		RetJavaTypes:    retArgTypes,
		Returns:         len(method.OutArgs) >= 1,
		SendType:        javaType(method.OutStream, true, env),
		Doc:             toJavaString(method.NamePos.Doc),
		Tags:            tags,
	}
}

func processServerWrapperEmbedMethod(iface *compile.Interface, embedMethod *compile.Method, env *compile.Env) serverWrapperEmbedMethod {
	return serverWrapperEmbedMethod{
		AccessModifier:      accessModifierForName(embedMethod.Name),
		CallingArgs:         javaCallingArgStr(embedMethod.InArgs, true),
		DeclarationArgs:     javaDeclarationArgStr(embedMethod.InArgs, env, true),
		LocalWrapperVarName: vdlutil.FirstRuneToLower(iface.Name) + "Wrapper",
		Name:                vdlutil.FirstRuneToLower(embedMethod.Name),
		RetType:             serverInterfaceOutArg(iface, embedMethod, env),
		Returns:             len(embedMethod.OutArgs) >= 1,
	}
}

// genJavaServerWrapperFile generates a java file containing a server wrapper for the specified
// interface.
func genJavaServerWrapperFile(iface *compile.Interface, env *compile.Env) JavaFileInfo {
	embeds := []serverWrapperEmbed{}
	for _, embed := range allEmbeddedIfaces(iface) {
		embeds = append(embeds, serverWrapperEmbed{
			WrapperClassName:    javaPath(javaGenPkgPath(path.Join(embed.File.Package.GenPath, vdlutil.FirstRuneToUpper(embed.Name+"ServerWrapper")))),
			LocalWrapperVarName: vdlutil.FirstRuneToLower(embed.Name) + "Wrapper",
		})
	}
	methodTags := make(map[string][]methodTag)
	// Copy method tags off of the interface.
	methods := make([]serverWrapperMethod, len(iface.Methods))
	for i, method := range iface.Methods {
		tags := make([]methodTag, len(method.Tags))
		for j, tag := range method.Tags {
			tags[j].Value = javaConstVal(tag, env)
			tags[j].Type = javaReflectType(tag.Type(), env)
		}
		methodTags[vdlutil.FirstRuneToLower(method.Name)] = tags
		methods[i] = processServerWrapperMethod(iface, method, env, tags)
	}
	embedMethods := []serverWrapperEmbedMethod{}
	for _, embedMao := range dedupedEmbeddedMethodAndOrigins(iface) {
		embedMethods = append(embedMethods, processServerWrapperEmbedMethod(embedMao.Origin, embedMao.Method, env))
	}
	javaServiceName := vdlutil.FirstRuneToUpper(iface.Name)
	data := struct {
		FileDoc         string
		AccessModifier  string
		EmbedMethods    []serverWrapperEmbedMethod
		Embeds          []serverWrapperEmbed
		FullServiceName string
		Methods         []serverWrapperMethod
		MethodTags      map[string][]methodTag
		PackagePath     string
		ServiceName     string
		Source          string
		Doc             string
	}{
		FileDoc:         iface.File.Package.FileDoc,
		AccessModifier:  accessModifierForName(iface.Name),
		EmbedMethods:    embedMethods,
		Embeds:          embeds,
		FullServiceName: javaPath(interfaceFullyQualifiedName(iface)),
		Methods:         methods,
		MethodTags:      methodTags,
		PackagePath:     javaPath(javaGenPkgPath(iface.File.Package.GenPath)),
		ServiceName:     javaServiceName,
		Source:          iface.File.BaseName,
		Doc:             toJavaString(iface.NamePos.Doc),
	}
	var buf bytes.Buffer
	err := parseTmpl("server wrapper", serverWrapperTmpl).Execute(&buf, data)
	if err != nil {
		log.Fatalf("vdl: couldn't execute server wrapper template: %v", err)
	}
	return JavaFileInfo{
		Name: javaServiceName + "ServerWrapper.java",
		Data: buf.Bytes(),
	}
}

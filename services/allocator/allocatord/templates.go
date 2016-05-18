// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"html/template"
	"net/http"

	"v.io/v23/context"
)

var homeTmpl = template.Must(template.New("main").Parse(`<!doctype html>
<html>
<head>
  <title>Vanadium Allocator</title>
</head>

<body>
  <script>
    function changeBtn(id, newHTML) {
      document.getElementById(id).innerHTML = "<font color='gray'>"+newHTML+"</font>";
    }
  </script>
  <main>
    <font face="courier">
    <h1>Create and Manage Instances of <<{{.ServerName}}>></h1>
    <p>
      <font size="2" color="gray">Logged in as: <b>{{.Email}}</b></font>
    </p>
    <p>
      Your instances:
    </p>
    {{range $index, $element := .Instances}}
       {{.Name}}  [<span id="destroyBtn{{$index}}"><a href="{{.DestroyURL}}" onclick="changeBtn('destroyBtn{{$index}}', 'Destroying (takes a few seconds) ...')">Destroy</a></span>]<br/>
    {{else}}
       None found.
    {{end}}
    <p>
      [<span id="createBtn"><a href="{{.CreateURL}}" onclick="changeBtn('createBtn', 'Creating (takes a few seconds) ...')">Create New</a></span>]
    </p>
    {{with .Message -}}
    <hr>
    <p>
      <font size="2">{{.}}</font>
    </p>
    {{end}}
    </font>
  </main>
</body>
</html>`))

var badRequestTmpl = template.Must(template.New("Bad Request").Parse(`<!doctype html>
<html>
<head>
  <title>Bad Request</title>
</head>
<body>
  <h1>Bad Request</h1>
  <pre>
    {{.Path}}
  </pre>
  {{with .Error -}}
  <p>
    Error: {{.}}.
  </p>
  {{end}}
</body>
</html>`))

func badRequest(ctx *context.T, w http.ResponseWriter, r *http.Request, err error) {
	tmplArgs := struct {
		Path  string
		Error error
	}{
		Path:  r.URL.Path,
		Error: err,
	}
	if err := badRequestTmpl.Execute(w, tmplArgs); err != nil {
		ctx.Errorf("Failed to execute template: %v", err)
	}
}

var errorTmpl = template.Must(template.New("Error").Parse(`<!doctype html>
<html>
<head>
  <title>Error</title>
</head>
<body>
  <h1>An Error Occurred</h1>
  <pre>
    {{.URL}}
  </pre>
  {{with .Error -}}
  <p>
    Error: {{.}}.
  </p>
  {{end}}
  <p>
    <a href="{{.Home}}">Home</a>
  </p>
</body>
</html>`))

func errorOccurred(ctx *context.T, w http.ResponseWriter, r *http.Request, homeURL string, err error) {
	tmplArgs := struct {
		URL, Home string
		Error     error
	}{
		URL:   r.URL.String(),
		Home:  homeURL,
		Error: err,
	}
	if err := errorTmpl.Execute(w, tmplArgs); err != nil {
		ctx.Errorf("Failed to execute template: %v", err)
	}
}

var rootTmpl = template.Must(template.New("Root").Parse(`<!doctype html>
<html>
<head>
  <title>Vanadium Allocator</title>
</head>
<body>
  <font face="courier">
  <h1>Create and Manage Instances of <<{{.ServerName}}>></h1>
  <p>
    <a href="{{.Home}}">Log in</a>
  </p>
  </font>
</body>
</html>`))

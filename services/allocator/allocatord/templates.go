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
       {{.Name}}  [<span id="destroyBtn{{$index}}"><a href="{{.DestroyURL}}" onclick="changeBtn('destroyBtn{{$index}}', 'Destroying (takes a few seconds) ...')">Destroy</a></span>][<a href="{{.DashboardURL}}" target="_blank">Dashboard</a>]<br/>
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

var dashboardTmpl = template.Must(template.New("Dashboard").Parse(`<!DOCTYPE html>
<html>
  <head>
    <title>{{.ServerName}} Dashboard</title>

    <link href='//fonts.googleapis.com/css?family=Source+Code+Pro:400,500|Roboto:500,400italic,300,500italic,300italic,400'
      rel='stylesheet'
      type='text/css'>
    <script src="https://ajax.googleapis.com/ajax/libs/jquery/2.2.2/jquery.min.js"></script>
    <script src="https://cdnjs.cloudflare.com/ajax/libs/jquery-url-parser/2.3.1/purl.min.js"></script>
    <script src="https://cdnjs.cloudflare.com/ajax/libs/jquery-cookie/1.4.1/jquery.cookie.min.js"></script>
    <link href="static/style.css" rel="stylesheet">
    <script src="static/dash.js"></script>
    <script type="text/javascript" src="https://www.gstatic.com/charts/loader.js"></script>
    <script type="text/javascript">
      google.charts.load('current', {packages: ['corechart']});
      google.charts.setOnLoadCallback(dash.init);
    </script>
  </head>
  <body>
    <div id="container">
      <div id="header">
        <div id="title">{{.ServerName}} Dashboard</div>
        <div>{{.Instance}}</div>
        <div id="email">{{.Email}}</div>
      </div>
      <div id="charts">
        <div class="chart" id="latency"></div>
        <div class="chart" id="qps"></div>
        <div class="chart" id="cpu-usage-pct"></div>
        <div class="chart" id="mem-usage-pct"></div>
        <div class="chart" id="disk-usage-pct"></div>
      </div>
    </div>
    <div id="durations-container">
      <div id="loading-label">LOADING...</div>
      <div id="durations">
        <div class="duration-item selected">1h</div>
        <div class="duration-item">2h</div>
        <div class="duration-item">4h</div>
        <div class="duration-item">6h</div>
        <div class="duration-item">12h</div>
        <div class="duration-item">1d</div>
        <div class="duration-item">7d</div>
      </div>
    </div>
    <div id="error-msg">
      Failed to retrieve data. Please try again later.
    </div>
  </body>
</html>`))

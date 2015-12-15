// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/services/logreader"
	"v.io/v23/services/stats"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
)

func init() {
	cmdBrowse.Flags.StringVar(&flagBrowseAddr, "addr", "", "Address on which the interactive HTTP server will listen. For example, localhost:14141. If empty, defaults to localhost:<some random port>")
	cmdBrowse.Flags.BoolVar(&flagBrowseLog, "log", true, "If true, log debug data obtained so that if a subsequent refresh from the browser fails, previously obtained information is available from the log file")
}

var (
	flagBrowseAddr string
	flagBrowseLog  bool
	cmdBrowse      = &cmdline.Command{
		Runner: v23cmd.RunnerFunc(runBrowse),
		Name:   "browse",
		Short:  "Starts an interactive interface for debugging",
		Long: `
Starts a webserver with a URL that when visited allows for inspection of a
remote process via a web browser.

This differs from browser.v.io in a few important ways:

  (a) Does not require a chrome extension,
  (b) Is not tied into the v.io cloud services
  (c) Can be setup with alternative different credentials,
  (d) The interface is more geared towards debugging a server than general purpose namespace browsing.

While (d) is easily overcome by sharing code between the two, (a), (b) & (c)
are not easy to work around.  Of course, the down-side here is that this
requires explicit command-line invocation instead of being just a URL anyone
can visit (https://browser.v.io).

A dump of some possible future features:
TODO(ashankar):?

  (1) Profiling: Should be able to use the webserver to profile the remote
  process (via 'go tool pprof' for example).  In the mean time, use the 'pprof'
  command (instead of the 'browse' command) for this purpose.
  (2) Trace browsing: Browse traces at the remote server, and possible force
  the collection of some traces (avoiding the need to restart the remote server
  with flags like --v23.vtrace.collect-regexp for example). In the mean time,
  use the 'vtrace' command (instead of the 'browse' command) for this purpose.
  (3) Log offsets: Log files can be large and currently the logging endpoint
  of this interface downloads the full log file from the beginning. The ability
  to start looking at the logs only from a specified offset might be useful
  for these large files.
  (4) Delegation: The 'browse' command requires the appropriate credentials to
  inspect a remote process. Make delegation of these credentials to another
  instance of the 'browse' command easier so that, for example, Bob can conveniently
  ask Alice to debug his service without worrying about giving Alice the ability to
  modify his service.
  (5) Signature: Display the interfaces, types etc. defined by any suffix in the
  remote process. in the mean time, use the 'vrpc signature' command for this purpose.
`,
		ArgsName: "<name>",
		ArgsLong: "<name> is the vanadium object name of the remote process to inspec",
	}
)

func runBrowse(ctx *context.T, env *cmdline.Env, args []string) error {
	if got, want := 1, len(args); got != want {
		return env.UsageErrorf("interactive: must provide a single vanadium object name")
	}
	http.Handle("/", &resolveHandler{ctx})
	http.Handle("/stats", &statsHandler{ctx})
	http.Handle("/blessings", &blessingsHandler{ctx})
	http.Handle("/logs", &logsHandler{ctx})
	http.Handle("/glob", &globHandler{ctx})
	http.Handle("/favicon.ico", http.NotFoundHandler())
	addr := flagBrowseAddr
	if len(addr) == 0 {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go http.Serve(ln, nil)
	url := "http://" + ln.Addr().String() + "/?n=" + url.QueryEscape(args[0])
	fmt.Printf("Visit %s and Ctrl-C to quit\n", url)
	// Open the browser if we can
	switch runtime.GOOS {
	case "linux":
		exec.Command("xdg-open", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	}
	<-signals.ShutdownOnSignals(ctx)
	return nil
}

func executeTemplate(ctx *context.T, w http.ResponseWriter, r *http.Request, tmpl *template.Template, args interface{}) {
	if flagBrowseLog {
		ctx.Infof("DEBUG: %q -- %+v", r.URL, args)
	}
	if err := tmpl.Execute(w, args); err != nil {
		w.Write([]byte(fmt.Sprintf("ERROR:%v", err)))
		ctx.Errorf("Error executing template %q: %v", tmpl.Name(), err)
	}
}

// Tracer forces collection of a trace rooted at the call to newTracer.
type Tracer struct {
	ctx  *context.T
	span vtrace.Span
}

func newTracer(ctx *context.T) (*context.T, *Tracer) {
	// TODO(ashankar): Should we set a timeout on the context, or maybe set a timeout
	// in the clients - based on what they are doing?
	ctx, span := vtrace.WithNewTrace(ctx)
	vtrace.ForceCollect(ctx)
	return ctx, &Tracer{ctx, span}
}

func (t *Tracer) String() string {
	if t == nil {
		return ""
	}
	tr := vtrace.GetStore(t.ctx).TraceRecord(t.span.Trace())
	if len(tr.Spans) == 0 {
		// Do not bother with empty traces
		return ""
	}
	var buf bytes.Buffer
	// nil as the time.Location is fine because the HTTP "server" time is
	// the same as that of the "client" (typically a browser on localhost).
	vtrace.FormatTrace(&buf, vtrace.GetStore(t.ctx).TraceRecord(t.span.Trace()), nil)
	return buf.String()
}

type resolveHandler struct{ ctx *context.T }

func (h *resolveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("n")
	var suffix string
	ctx, tracer := newTracer(h.ctx)
	m, err := v23.GetNamespace(ctx).Resolve(ctx, name)
	if m != nil {
		suffix = m.Name
	}
	args := struct {
		ServerName  string
		CommandLine string
		Vtrace      *Tracer
		MountEntry  *naming.MountEntry
		Error       error
	}{
		ServerName:  strings.TrimSuffix(name, suffix),
		CommandLine: fmt.Sprintf("debug resolve %q", name),
		Vtrace:      tracer,
		MountEntry:  m,
		Error:       err,
	}
	executeTemplate(h.ctx, w, r, tmplBrowseResolve, args)
}

type blessingsHandler struct{ ctx *context.T }

func (h *blessingsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("n")
	ctx, tracer := newTracer(h.ctx)
	call, err := v23.GetClient(ctx).StartCall(ctx, name, "DoNotReallyCareAboutTheMethod", nil)
	args := struct {
		ServerName        string
		CommandLine       string
		Vtrace            *Tracer
		Error             error
		Blessings         security.Blessings
		Recognized        []string
		CertificateChains [][]security.Certificate
	}{
		ServerName:  name,
		Vtrace:      tracer,
		CommandLine: fmt.Sprintf("vrpc identify %q", name),
		Error:       err,
	}
	if call != nil {
		args.Recognized, args.Blessings = call.RemoteBlessings()
		args.CertificateChains = security.MarshalBlessings(args.Blessings).CertificateChains
		// Don't actually care about the RPC, so don't bother waiting on the Finish.
		defer func() { go call.Finish() }()
	}
	executeTemplate(h.ctx, w, r, tmplBrowseBlessings, args)
}

type statsHandler struct{ ctx *context.T }

func (h *statsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		server      = r.FormValue("n")
		stat        = r.FormValue("s")
		prefix      = naming.Join(server, "__debug", "stats")
		name        = naming.Join(prefix, stat)
		ctx, tracer = newTracer(h.ctx)
	)
	v, err := stats.StatsClient(name).Value(ctx)
	var children []string
	var childrenErrors []error
	if verror.ErrorID(err) == verror.ErrNoExist.ID {
		// The stat itself isn't readable, maybe it is globable?
		if glob, globErr := v23.GetNamespace(ctx).Glob(ctx, naming.Join(name, "*")); globErr == nil {
			for e := range glob {
				switch e := e.(type) {
				case *naming.GlobReplyEntry:
					children = append(children, strings.TrimPrefix(e.Value.Name, prefix))
				case *naming.GlobReplyError:
					childrenErrors = append(childrenErrors, e.Value.Error)
				}
			}
			if len(children) == 1 {
				// Single child, save an extra click
				redirect, err := url.Parse(r.URL.String())
				if err == nil {
					q := redirect.Query()
					q.Set("n", server)
					q.Set("s", children[0])
					redirect.RawQuery = q.Encode()
					ctx.Infof("Redirecting from %v to %v", r.URL, redirect)
					http.Redirect(w, r, redirect.String(), http.StatusTemporaryRedirect)
					return
				}
			}
		} else {
			err = globErr
		}
	}
	args := struct {
		ServerName     string
		CommandLine    string
		Vtrace         *Tracer
		StatName       string
		Value          *vdl.Value
		Children       []string
		ChildrenErrors []error
		Globbed        bool
		Error          error
	}{
		ServerName:     server,
		Vtrace:         tracer,
		StatName:       stat,
		Value:          v,
		Error:          err,
		Children:       children,
		ChildrenErrors: childrenErrors,
		Globbed:        len(children)+len(childrenErrors) > 0,
	}
	if args.Globbed {
		args.CommandLine = fmt.Sprintf("debug glob %q", naming.Join(name, "*"))
	} else {
		args.CommandLine = fmt.Sprintf("debug stats read %q", name)
	}
	executeTemplate(h.ctx, w, r, tmplBrowseStats, args)
}

type logsHandler struct{ ctx *context.T }

func (h *logsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		server = r.FormValue("n")
		log    = r.FormValue("l")
		prefix = naming.Join(server, "__debug", "logs")
		name   = naming.Join(prefix, log)
		path   = r.URL.Path
		list   = func() bool {
			for _, a := range r.Header[http.CanonicalHeaderKey("Accept")] {
				if a == "text/event-stream" {
					return true
				}
			}
			return false
		}()
		ctx, _ = newTracer(h.ctx)
	)
	// The logs handler streams result to the web browser because there
	// have been cases where there are ~1 million log files, so doing this
	// streaming thing will make the UI more responsive.
	if len(log) == 0 && list {
		w.Header().Add("Content-Type", "text/event-stream")
		glob, err := v23.GetNamespace(ctx).Glob(ctx, naming.Join(name, "*"))
		if err != nil {
			writeErrorEvent(w, err)
			return
		}
		flusher, _ := w.(http.Flusher)
		for e := range glob {
			switch e := e.(type) {
			case *naming.GlobReplyEntry:
				logfile := strings.TrimPrefix(e.Value.Name, prefix+"/")
				writeEvent(w, fmt.Sprintf(
					`<a href="%s?n=%s&l=%s">%s</a>`,
					path,
					url.QueryEscape(server),
					url.QueryEscape(logfile),
					html.EscapeString(logfile)))
			case *naming.GlobReplyError:
				writeErrorEvent(w, e.Value.Error)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		return
	}
	if len(log) == 0 {
		args := struct {
			ServerName  string
			CommandLine string
			Vtrace      *Tracer
		}{
			ServerName:  server,
			CommandLine: fmt.Sprintf("debug glob %q", naming.Join(name, "*")),
		}
		executeTemplate(h.ctx, w, r, tmplBrowseLogsList, args)
	}
	w.Header().Add("Content-Type", "text/plain")
	stream, err := logreader.LogFileClient(name).ReadLog(h.ctx, 0, logreader.AllEntries, true)
	if err != nil {
		w.Write([]byte(fmt.Sprintf("ERROR(%v): %v\n", verror.ErrorID(err), err)))
		return
	}
	var (
		entries   = make(chan logreader.LogEntry)
		abortRPC  = make(chan bool)
		abortHTTP <-chan bool
		errch     = make(chan error, 1) // At most one write on this channel, avoid blocking any goroutines
	)
	if notifier, ok := w.(http.CloseNotifier); ok {
		abortHTTP = notifier.CloseNotify()
	}
	go func() {
		// writes to: entries, errch
		// reads from: abortRPC
		defer stream.Finish()
		defer close(entries)
		iterator := stream.RecvStream()
		for iterator.Advance() {
			select {
			case entries <- iterator.Value():
			case <-abortRPC:
				return
			}
		}
		if err := iterator.Err(); err != nil {
			errch <- err
		}
	}()
	// reads from: entries, errch, abortHTTP
	// writes to: abortRPC
	defer close(abortRPC)
	for {
		select {
		case e, more := <-entries:
			w.Write([]byte(e.Line))
			w.Write([]byte("\n"))
			if !more {
				return
			}
		case err := <-errch:
			w.Write([]byte(fmt.Sprintf("ERROR(%v): %v\n", verror.ErrorID(err), err)))
			return
		case <-abortHTTP:
			return
		}
	}
}

type globHandler struct{ ctx *context.T }

func (h *globHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Suffix string
		Error  error
	}
	var (
		server      = r.FormValue("n")
		suffix      = r.FormValue("s")
		pattern     = naming.Join(server, suffix, "*")
		ctx, tracer = newTracer(h.ctx)
		entries     []entry
	)
	ch, err := v23.GetNamespace(ctx).Glob(ctx, pattern)
	if err != nil {
		entries = append(entries, entry{Error: err})
	}
	if ch != nil {
		for e := range ch {
			switch e := e.(type) {
			case *naming.GlobReplyEntry:
				entries = append(entries, entry{Suffix: strings.TrimPrefix(e.Value.Name, server)})
			case *naming.GlobReplyError:
				entries = append(entries, entry{Error: e.Value.Error})
			}
		}
	}
	args := struct {
		ServerName  string
		CommandLine string
		Vtrace      *Tracer
		Pattern     string
		Entries     []entry
	}{
		ServerName:  server,
		CommandLine: fmt.Sprintf("debug glob %q", pattern),
		Vtrace:      tracer,
		Pattern:     pattern,
		Entries:     entries,
	}
	executeTemplate(h.ctx, w, r, tmplBrowseGlob, args)
}

func writeEvent(w http.ResponseWriter, data string) {
	w.Write([]byte(fmt.Sprintf("data: %s\n\n", strings.TrimSpace(data))))
}

func writeErrorEvent(w http.ResponseWriter, err error) {
	id := fmt.Sprintf("%v", verror.ErrorID(err))
	writeEvent(w, fmt.Sprintf("ERROR(%v): %v", html.EscapeString(id), html.EscapeString(err.Error())))
}

func makeTemplate(name, content string) *template.Template {
	content = "{{template `.header` .}}" + content + "{{template `.footer` .}}"
	t := template.Must(tmplBrowseHeader.Clone())
	t = template.Must(t.AddParseTree(tmplBrowseFooter.Name(), tmplBrowseFooter.Tree))
	t = t.Funcs(template.FuncMap{
		"verrorID":           verror.ErrorID,
		"unmarshalPublicKey": security.UnmarshalPublicKey,
		"endpoint": func(n string) (naming.Endpoint, error) {
			if naming.Rooted(n) {
				n, _ = naming.SplitAddressName(n)
			}
			return v23.NewEndpoint(n)
		},
		"endpointName": func(ep naming.Endpoint) string { return ep.Name() },
		"goValueFromVDL": func(v *vdl.Value) interface{} {
			var ret interface{}
			vdl.Convert(&ret, v)
			return ret
		},
	})
	t = template.Must(t.New(name).Parse(content))
	return t
}

var (
	tmplBrowseResolve = makeTemplate("resolve", `
<section class="section--center mdl-grid">
  <h5>Name resolution</h5>
  <div class="mdl-cell mdl-cell--12-col">
{{with .MountEntry}}
    <ul>
    {{with .Name}}<li>Suffix: {{.}}</li>{{end}}
    {{with .ServesMountTable}}<li>This server is a mounttable</li>{{end}}
    {{with .IsLeaf}}<li>This is a leaf server</li>{{end}}
    </ul>
  {{range .Servers}}
    <section class="section--center mdl-grid">
    <div class="mdl-cell mdl-cell--12-col">
    <table class="mdl-data-table mdl-js-data-table mdl-data-table--selectable mdl-shadow--2dp">
    <tbody>
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Endpoint</td>
      <td class="mdl-data-table__cell--non-numeric"><a href="/?n={{endpoint .Server | endpointName | urlquery}}">{{.Server}}</a></td>
    </tr>
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Expires</td>
      <td class="mdl-data-table__cell--non-numeric">{{.Deadline}}</td>
    </tr>
    {{with $ep := endpoint .Server}}
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Network</td>
      <td class="mdl-data-table__cell--non-numeric">{{.Addr.Network}}</td>
    </tr>
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Address</td>
      <td class="mdl-data-table__cell--non-numeric">{{.Addr}}</td>
    </tr>
    <tr>
      <td class="mdl-data-table__cell--non-numeric">RoutingID</td>
      <td class="mdl-data-table__cell--non-numeric">{{.RoutingID}}</td>
    </tr>
    {{with .BlessingNames}}
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Blessings ({{len .}})</td>
      <td class="mdl-data-table__cell--non-numeric">{{.}}</td>
    </tr>
    {{end}}
    {{with .Routes}}
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Routes ({{len .}})</td>
      <td class="mdl-data-table__cell--non-numeric">{{.}}</td>
    </tr>
    {{end}}
    {{end}}
    </tbody>
    </table>
    {{end}}
    </div>
    </section>
{{else}}
  Name resolution came up empty
{{end}}
</div>
</section>

{{with .Error}}
<section class="section--center mdl-grid">
  <h5><i class="material-icons">info</i>ERROR({{verrorID .}})</h5>
  <div class="mdl-cell mdl-cell--12-col fixed-width">{{.}}</div>
</section>
{{end}}
`)

	tmplBrowseBlessings = makeTemplate("blessings", `
<section class="section--center mdl-grid">
  <h5>Claimed</h5>
  <div class="mdl-cell mdl-cell--12-col">
  {{.Blessings}}
  </div>
</section>

{{with .Recognized}}
<section class="section--center mdl-grid">
  <h5>Recognized</h5>
  <div class="mdl-cell mdl-cell--12-col">
  <ul>
  {{range .}}
  <li>{{.}}</li>
  {{end}}
  </ul>
  </div>
</section>
{{end}}

<section class="section--center mdl-grid">
  <h5>PublicKey</h5>
  <div class="mdl-cell mdl-cell--12-col fixed-width">
  {{.Blessings.PublicKey}}
  </div>
</section>

{{with .CertificateChains}}
<section class="section--center mdl-grid">
  <h5>Certificate Chains (Total: {{len .}})</h5>
  <div class="mdl-cell mdl-cell--12-col">
  {{range $chainidx, $chain := .}}
  <section class="section--center mdl-grid">
    <h6>Chain #{{$chainidx}}</h6>
    <div class="mdl-cell mdl-cell--12-col">
    <table class="mdl-data-table mdl-js-data-table mdl-data-table--selectable mdl-shadow--2dp">
    <thead>
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Certificate</td>
      <td class="mdl-data-table__cell--non-numeric">Extension</td>
      <td class="mdl-data-table__cell--non-numeric">Blessed Public Key</td>
      <td class="mdl-data-table__cell--non-numeric">Caveats</td>
    </tr>
    </thead>
    <tbody>
    {{range $certidx, $cert := $chain}}
    <tr>
      <td>{{$certidx}}</td>
      <td class="mdl-data-table__cell--non-numeric">{{$cert.Extension}}</td>
      <td class="mdl-data-table__cell--non-numeric fixed-width">{{unmarshalPublicKey $cert.PublicKey}}</td>
      <td class="mdl-data-table__cell--non-numeric">{{len $cert.Caveats}}
      {{range $cavidx, $cav := $cert.Caveats}}
      <br/>#{{$cavidx}}: {{$cav}}
      {{end}}
      </td>
    </tr>
    {{end}}
    </tbody>
    </table>
    </div>
  </section>
  {{end}}
  </div>
</section>
{{end}}

{{with .Error}}
<section class="section--center mdl-grid">
  <h5><i class="material-icons">info</i>ERROR({{verrorID .}})</h5>
  <div class="mdl-cell mdl-cell--12-col fixed-width">{{.}}</div>
</section>
{{end}}
`)

	tmplBrowseStats = makeTemplate("stats", `
{{if .Globbed}}
<section class="section--center mdl-grid">
  <h5>Glob</h5>
  <div class="mdl-cell mdl-cell--12-col">
  <ul>
  {{range .Children}}
  <li><a href="/stats?n={{urlquery $.ServerName}}&s={{urlquery .}}">{{.}}</a></li>
  {{end}}
  {{range .ChildrenErrors}}
  <li>ERROR({{verrorID .}}): {{.}}</li>
  {{end}}
  </ul>
  </div>
</section>
{{end}}

{{if .Value}}
<section class="section--center mdl-grid">
  <div class="mdl-cell mdl-cell--12-col">
    <table class="mdl-data-table mdl-js-data-table mdl-data-table--selectable mdl-shadow--2dp">
    <tbody>
    {{with $goVal := goValueFromVDL .Value}}
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Value (Go)</td>
      <td class="mdl-data-table__cell--non-numeric"><pre>{{$goVal}}</pre></td>
    </tr>
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Type (Go)</td>
      <td class="mdl-data-table__cell--non-numeric fixed-width">{{printf "%T" $goVal}}</td>
    </tr>
    {{else}}
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Value (VDL)</td>
      <td class="mdl-data-table__cell--non-numeric fixed-width">{{.Value}}</td>
    </tr>
    {{end}}
    <tr>
      <td class="mdl-data-table__cell--non-numeric">Type (VDL)</td>
      <td class="mdl-data-table__cell--non-numeric fixed-width">{{.Value.Type}}</td>
    </tr>
    </tbody>
    </table>
  </div>
</section>
{{end}}

{{if not .Globbed}}
{{with .Error}}
<section class="section--center mdl-grid">
  <h5><i class="material-icons">info</i>ERROR({{verrorID .}})</h5>
  <div class="mdl-cell mdl-cell--12-col fixed-width">{{.}}</div>
</section>
{{end}}
{{end}}
`)

	tmplBrowseLogsList = makeTemplate("logs", `
<section class="section--center mdl-grid">
  <h5>List of log files</h5>
  <div id="parent" class="mdl-cell mdl-cell--12-col">
  <script>
  var source = new EventSource("/logs?n={{urlquery .ServerName}}");
  source.onmessage = function(event) {
    var ol = document.getElementById("logfiles");
    var li = document.createElement("li");
    li.innerHTML = event.data;
    ol.appendChild(li);
  }
  source.onerror = function() {
    source.close();
    document.getElementById("parent").removeChild(document.getElementById("progress"));
  }
  </script>
  <div id="progress" class="mdl-progress mdl-js-progress mdl-progress__indeterminate"></div>
  <ol id="logfiles">
  </ol>
  </div>
</section>
`)

	tmplBrowseGlob = makeTemplate("glob", `
<section class="section--center mdl-grid">
  <h5>{{.Pattern}}</h5>
  <div id="parent" class="mdl-cell mdl-cell--12-col">
  <ol>
  {{range .Entries}}
    {{with .Suffix}}
    <li><a href="/glob?n={{urlquery $.ServerName}}&s={{urlquery .}}">{{.}}</a></li>
    {{end}}
    {{with .Error}}
    <li>ERROR({{verrorID .}}): {{.}}</li>
    {{end}}
  {{end}}
  </ol>
  </div>
</section>
`)

	tmplBrowseHeader = template.Must(template.New(".header").Parse(`
{{define ".header"}}
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
    <title>Debugging {{.ServerName}}</title>
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <link rel="stylesheet" href="https://storage.googleapis.com/code.getmdl.io/1.0.6/material.teal-blue.min.css">
    <script src="https://storage.googleapis.com/code.getmdl.io/1.0.6/material.min.js"></script>
    <link rel="stylesheet" href="https://fonts.googleapis.com/icon?family=Material+Icons">
    <style>
    .fixed-width { font-family: monospace; }
    </style>
</head>
<body>
  <!-- Always shows a header, even in smaller screens. -->
  <div class="mdl-layout mdl-js-layout mdl-layout--fixed-header">
    <header class="mdl-layout__header">
      <div class="mdl-layout__header-row">
        <!-- Title -->
        <span class="mdl-layout-title">Debug</span>
        <!-- Add spacer, to align navigation to the right -->
        <div class="mdl-layout-spacer"></div>
        <!-- Navigation. We hide it in small screens. -->
        <nav class="mdl-navigation mdl-layout--large-screen-only">
          <a class="mdl-navigation__link" href="/?n={{.ServerName}}">Name</a>
          <a class="mdl-navigation__link" href="/blessings?n={{.ServerName}}">Blessings</a>
          <a class="mdl-navigation__link" href="/stats?n={{.ServerName}}">Stats</a>
          <a class="mdl-navigation__link" href="/logs?n={{.ServerName}}">Logs</a>
          <a class="mdl-navigation__link" href="/glob?n={{.ServerName}}">Glob</a>
        </nav>
      </div>
    </header>
    <div class="mdl-layout__drawer">
      <nav class="mdl-navigation">
        <a class="mdl-navigation__link" href="/?n={{.ServerName}}">Name</a>
        <a class="mdl-navigation__link" href="/blessings?n={{.ServerName}}">Blessings</a>
        <a class="mdl-navigation__link" href="/stats?n={{.ServerName}}">Stats</a>
        <a class="mdl-navigation__link" href="/logs?n={{.ServerName}}">Logs</a>
        <a class="mdl-navigation__link" href="/glob?n={{.ServerName}}">Glob</a>
      </nav>
    </div>
    <main class="mdl-layout__content">
    <section class="section--center mdl-grid">
    <h5 class="fixed-width">{{.ServerName}}</h5>
    </section>
{{end}}
`))
	tmplBrowseFooter = template.Must(template.New(".footer").Parse(`
{{define ".footer"}}
    <hr/>
    {{with .CommandLine}}
    <section class="section--center mdl-grid">
    <h5>CommandLine</h5>
    <div class="mdl-cell mdl-cell--12-col fixed-width">{{.}}</div>
    </section>
    {{end}}
    {{with printf "%v" .Vtrace}}
    <section class="section--center mdl-grid">
    <h5>Trace</h5>
    <div class="mdl-cell mdl-cell--12-col"><pre>{{.}}</pre></div>
    </section>
    {{end}}
    </main>
  </div>
</body>
</html>
{{end}}
`))
)

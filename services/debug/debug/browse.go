// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"html"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/services/logreader"
	"v.io/v23/services/stats"
	svtrace "v.io/v23/services/vtrace"
	"v.io/v23/uniqueid"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	"v.io/x/lib/cmdline"
	seclib "v.io/x/ref/lib/security"
	"v.io/x/ref/lib/signals"
	"v.io/x/ref/lib/v23cmd"
	"v.io/x/ref/services/internal/pproflib"
)

func init() {
	cmdBrowse.Flags.StringVar(&flagBrowseAddr, "addr", "", "Address on which the interactive HTTP server will listen. For example, localhost:14141. If empty, defaults to localhost:<some random port>")
	cmdBrowse.Flags.BoolVar(&flagBrowseLog, "log", true, "If true, log debug data obtained so that if a subsequent refresh from the browser fails, previously obtained information is available from the log file")
	cmdBrowse.Flags.StringVar(&flagBrowseBlessings, "blessings", "", "If non-empty, points to the blessings required to debug the process. This is typically obtained via 'debug delegate' run by the owner of the remote process")
	cmdBrowse.Flags.StringVar(&flagBrowsePrivateKey, "key", "", "If non-empty, must be accompanied with --blessings with a value obtained via 'debug delegate' run by the owner of the remote process")
}

const browseProfilesPath = "/profiles"

var (
	flagBrowseAddr       string
	flagBrowseLog        bool
	flagBrowseBlessings  string
	flagBrowsePrivateKey string
	cmdBrowse            = &cmdline.Command{
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

  (1) Trace browsing: Browse traces at the remote server, and possible force
  the collection of some traces (avoiding the need to restart the remote server
  with flags like --v23.vtrace.collect-regexp for example). In the mean time,
  use the 'vtrace' command (instead of the 'browse' command) for this purpose.
  (2) Log offsets: Log files can be large and currently the logging endpoint
  of this interface downloads the full log file from the beginning. The ability
  to start looking at the logs only from a specified offset might be useful
  for these large files.
  (3) Signature: Display the interfaces, types etc. defined by any suffix in the
  remote process. in the mean time, use the 'vrpc signature' command for this purpose.
`,
		ArgsName: "<name>",
		ArgsLong: "<name> is the vanadium object name of the remote process to inspect",
	}

	cmdDelegate = &cmdline.Command{
		Runner: v23cmd.RunnerFunc(runDelegate),
		Name:   "delegate",
		Short:  "Create credentials to delegate debugging to another user",
		Long: `
Generates credentials (private key and blessings) required to debug remote
processes owned by the caller and prints out the command that the delegate can
run. The delegation limits the bearer of the token to invoke methods on the
remote process only if they have the "access.Debug" tag on them, and this token
is valid only for limited amount of time. For example, if Alice wants Bob to
debug a remote process owned by her for the next 2 hours, she runs:

  debug delegate my-friend-bob 2h myservices/myservice

And sends Bob the output of this command. Bob will then be able to inspect the
remote process as myservices/myservice with the same authorization as Alice.
`,
		ArgsName: "<to> <duration> [<name>]",
		ArgsLong: `
<to> is an identifier to provide to the delegate.

<duration> is the time period to delegate for (e.g., 1h for 1 hour)

<name> (optional) is the vanadium object name of the remote process to inspect
`,
	}
)

func runBrowse(ctx *context.T, env *cmdline.Env, args []string) error {
	if got, want := 1, len(args); got != want {
		return env.UsageErrorf("must provide a single vanadium object name")
	}
	if len(flagBrowsePrivateKey) != 0 {
		keybytes, err := base64.URLEncoding.DecodeString(flagBrowsePrivateKey)
		if err != nil {
			return fmt.Errorf("bad value for --key: %v", err)
		}
		key, err := x509.ParseECPrivateKey(keybytes)
		if err != nil {
			return fmt.Errorf("bad value for --key: %v", err)
		}
		principal, err := seclib.NewPrincipalFromSigner(security.NewInMemoryECDSASigner(key), nil)
		if err != nil {
			return fmt.Errorf("unable to use --key: %v", err)
		}
		if ctx, err = v23.WithPrincipal(ctx, principal); err != nil {
			return err
		}
	}
	if len(flagBrowseBlessings) != 0 {
		blessingbytes, err := base64.URLEncoding.DecodeString(flagBrowseBlessings)
		if err != nil {
			return fmt.Errorf("bad value for --blessings: %v", err)
		}
		var blessings security.Blessings
		if err := vom.Decode(blessingbytes, &blessings); err != nil {
			return fmt.Errorf("bad value for --blessings: %v", err)
		}
		if _, err := v23.GetPrincipal(ctx).BlessingStore().Set(blessings, security.AllPrincipals); err != nil {
			if len(flagBrowsePrivateKey) == 0 {
				return fmt.Errorf("cannot use --blessings, should --key have also been provided? (%v)", err)
			}
			return fmt.Errorf("cannot use --blessings: %v", err)
		}
	}
	http.Handle("/", &resolveHandler{ctx})
	http.Handle("/stats", &statsHandler{ctx})
	http.Handle("/blessings", &blessingsHandler{ctx})
	http.Handle("/logs", &logsHandler{ctx})
	http.Handle("/glob", &globHandler{ctx})
	http.Handle(browseProfilesPath, &profilesHandler{ctx})
	http.Handle(browseProfilesPath+"/", &profilesHandler{ctx})
	http.Handle("/vtraces", &allTracesHandler{ctx})
	http.Handle("/vtrace", &vtraceHandler{ctx})
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

func runDelegate(ctx *context.T, env *cmdline.Env, args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return env.UsageErrorf("got %d arguments, expecting 2 or 3", len(args))
	}
	extension := args[0]
	duration, err := time.ParseDuration(args[1])
	if err != nil {
		return env.UsageErrorf("failed to parse <duration>: %v", err)
	}
	name := "<name>"
	if len(args) == 3 {
		name = args[2]
	}
	// Create a new private key.
	pub, priv, err := seclib.NewPrincipalKey()
	if err != nil {
		return err
	}
	privbytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	// Create a blessings
	var caveats []security.Caveat
	if c, err := access.NewAccessTagCaveat(access.Resolve, access.Debug); err != nil {
		return err
	} else {
		caveats = append(caveats, c)
	}
	if c, err := security.NewExpiryCaveat(time.Now().Add(duration)); err != nil {
		return err
	} else {
		caveats = append(caveats, c)
	}
	blessing, err := v23.GetPrincipal(ctx).Bless(pub, v23.GetPrincipal(ctx).BlessingStore().Default(), extension, caveats[0], caveats[1:]...)
	if err != nil {
		return err
	}
	blessingbytes, err := vom.Encode(blessing)
	if err != nil {
		return err
	}
	fmt.Printf(`Your delegate will be seen as %v at your server. Ask them to run:

debug browse \
  --key=%q \
  --blessings=%q \
  %q
`,
		blessing,
		base64.URLEncoding.EncodeToString(privbytes),
		base64.URLEncoding.EncodeToString(blessingbytes),
		name)
	return nil
}

func executeTemplate(ctx *context.T, w http.ResponseWriter, r *http.Request, tmpl *template.Template, args interface{}) {
	if flagBrowseLog {
		ctx.Infof("DEBUG: %q -- %+v", r.URL, args)
	}
	if err := tmpl.Execute(w, args); err != nil {
		fmt.Fprintf(w, "ERROR:%v", err)
		ctx.Errorf("Error executing template %q: %v", tmpl.Name(), err)
	}
}

// Tracer forces collection of a trace rooted at the call to newTracer.
type Tracer struct {
	ctx  *context.T
	span vtrace.Span
}

func newTracer(ctx *context.T) (*context.T, *Tracer) {
	ctx, span := vtrace.WithNewTrace(ctx)
	vtrace.ForceCollect(ctx, 0)
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

func withTimeout(ctx *context.T) *context.T {
	ctx, _ = context.WithTimeout(ctx, timeout)
	return ctx
}

type resolveHandler struct{ ctx *context.T }

func (h *resolveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("n")
	var suffix string
	ctx, tracer := newTracer(h.ctx)
	m, err := v23.GetNamespace(ctx).Resolve(withTimeout(ctx), name)
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
	call, err := v23.GetClient(ctx).StartCall(withTimeout(ctx), name, "DoNotReallyCareAboutTheMethod", nil)
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
	v, err := stats.StatsClient(name).Value(withTimeout(ctx))
	var children []string
	var childrenErrors []error
	if verror.ErrorID(err) == verror.ErrNoExist.ID {
		// The stat itself isn't readable, maybe it is globable?
		if glob, globErr := v23.GetNamespace(ctx).Glob(withTimeout(ctx), naming.Join(name, "*")); globErr == nil {
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
	//
	// For the same reason, avoid setting a timeout.
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
		return
	}
	w.Header().Add("Content-Type", "text/plain")
	stream, err := logreader.LogFileClient(name).ReadLog(ctx, 0, logreader.AllEntries, true)
	if err != nil {
		fmt.Fprintf(w, "ERROR(%v): %v\n", verror.ErrorID(err), err)
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
			if !more {
				return
			}
			fmt.Fprintln(w, e.Line)
		case err := <-errch:
			fmt.Fprintf(w, "ERROR(%v): %v\n", verror.ErrorID(err), err)
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
	ch, err := v23.GetNamespace(ctx).Glob(withTimeout(ctx), pattern)
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

type profilesHandler struct{ ctx *context.T }

func (h *profilesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		server = r.FormValue("n")
		name   = naming.Join(server, "__debug", "pprof")
	)
	if len(server) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Must specify a server with the URL query parameter 'n'")
		return
	}
	if path := strings.TrimSuffix(r.URL.Path, "/"); path == strings.TrimSuffix(browseProfilesPath, "/") {
		urlPrefix := fmt.Sprintf("http://%s%s/pprof", r.Host, path)
		args := struct {
			ServerName  string
			CommandLine string
			Vtrace      *Tracer
			URLPrefix   string
		}{
			ServerName:  server,
			CommandLine: fmt.Sprintf("debug pprof run %q", name),
			URLPrefix:   urlPrefix,
		}
		executeTemplate(h.ctx, w, r, tmplBrowseProfiles, args)
		return
	}
	pproflib.PprofProxy(h.ctx, browseProfilesPath, name).ServeHTTP(w, r)
}

const (
	allTracesLabel       = ">0s"
	oneMsTraceLabel      = ">1ms"
	tenMsTraceLabel      = ">10ms"
	hundredMsTraceLabel  = ">100ms"
	oneSecTraceLabel     = ">1s"
	tenSecTraceLabel     = ">10s"
	hundredSecTraceLabel = ">100s"
)

var (
	labelToCutoff = map[string]float64{
		allTracesLabel:       0,
		oneMsTraceLabel:      0.001,
		tenMsTraceLabel:      0.01,
		hundredMsTraceLabel:  0.1,
		oneSecTraceLabel:     1,
		tenSecTraceLabel:     10,
		hundredSecTraceLabel: 100,
	}
	sortedLabels = []string{allTracesLabel, oneMsTraceLabel, tenMsTraceLabel, hundredMsTraceLabel, oneSecTraceLabel, tenSecTraceLabel, hundredSecTraceLabel}
)

type traceWithStart struct {
	Id    string
	Start time.Time
}

type traceSort []traceWithStart

func (t traceSort) Len() int           { return len(t) }
func (t traceSort) Less(i, j int) bool { return t[i].Start.After(t[j].Start) }
func (t traceSort) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }

func bucketTraces(ctx *context.T, name string) (map[string]traceSort, error) {
	stub := svtrace.StoreClient(name)

	call, err := stub.AllTraces(ctx)
	if err != nil {
		return nil, err
	}
	stream := call.RecvStream()
	var missingTraces []string
	bucketedTraces := map[string]traceSort{}
	for stream.Advance() {
		trace := stream.Value()
		node := vtrace.BuildTree(&trace)
		if node == nil {
			missingTraces = append(missingTraces, trace.Id.String())
			continue
		}
		startTime := findStartTime(node)
		endTime := findEndTime(node)
		traceNode := traceWithStart{Id: trace.Id.String(), Start: startTime}
		duration := endTime.Sub(startTime).Seconds()
		if endTime.IsZero() || startTime.IsZero() {
			// Set the duration so something small but greater than zero so it shows up in
			// the first bucket.
			duration = 0.0001
		}
		for l, cutoff := range labelToCutoff {
			if duration >= cutoff {
				bucketedTraces[l] = append(bucketedTraces[l], traceNode)
			}
		}
	}

	for _, v := range bucketedTraces {
		sort.Sort(v)
	}
	if err := stream.Err(); err != nil {
		return bucketedTraces, err
	}
	if err := call.Finish(); err != nil {
		return bucketedTraces, err
	}
	if len(missingTraces) > 0 {
		return bucketedTraces, fmt.Errorf("missing information for: %s", strings.Join(missingTraces, ","))
	}
	return bucketedTraces, nil
}

type allTracesHandler struct{ ctx *context.T }

func (a *allTracesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		server = r.FormValue("n")
		name   = naming.Join(server, "__debug", "vtrace")
	)
	if len(server) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Must specify a server with the URL query parameter 'n'")
		return
	}
	ctx, tracer := newTracer(a.ctx)
	buckets, err := bucketTraces(ctx, name)

	data := struct {
		Buckets      map[string]traceSort
		SortedLabels []string
		Err          error
		ServerName   string
		CommandLine  string
		Vtrace       *Tracer
	}{
		Buckets:      buckets,
		SortedLabels: sortedLabels,
		Err:          err,
		CommandLine:  fmt.Sprintf("debug vtraces %q", name),
		ServerName:   server,
		Vtrace:       tracer,
	}
	executeTemplate(a.ctx, w, r, tmplBrowseAllTraces, data)
}

type divTree struct {
	Id string
	// Start is a value from 0-100 which is the percentage of time into the parent span's duration
	// that this span started.
	Start int
	// Width is a value from 0-100 which is the percentage of time in the parent span's duration this span
	// took
	Width       int
	Name        string
	Annotations []annotation
	Children    []divTree
}

type annotation struct {
	// Position is a value from 0-100 which is the percentage of time into the span's duration that this
	// annotation occured.
	Position int
	Msg      string
}

func convertToTree(n *vtrace.Node, parentStart time.Time, parentEnd time.Time) *divTree {
	// If either start of end is missing, use the parent start/end.
	startTime := n.Span.Start
	if startTime.IsZero() {
		startTime = parentStart
	}

	endTime := n.Span.End
	if endTime.IsZero() {
		endTime = parentEnd
	}

	parentDuration := parentEnd.Sub(parentStart).Seconds()
	start := int(100 * startTime.Sub(parentStart).Seconds() / parentDuration)
	end := int(100 * endTime.Sub(parentStart).Seconds() / parentDuration)
	width := end - start
	if width == 0 {
		width = 1
	}
	top := &divTree{
		Id:    n.Span.Id.String(),
		Start: start,
		Width: width,
		Name:  n.Span.Name,
	}

	top.Annotations = make([]annotation, len(n.Span.Annotations))
	for i, a := range n.Span.Annotations {
		top.Annotations[i].Msg = a.Message
		if a.When.IsZero() {
			top.Annotations[i].Position = 0
			continue
		}
		top.Annotations[i].Position = int(100*a.When.Sub(parentStart).Seconds()/parentDuration) - start
	}

	top.Children = make([]divTree, len(n.Children))
	for i, c := range n.Children {
		top.Children[i] = *convertToTree(c, startTime, endTime)
	}
	return top

}

// findStartTime returns the start time of a node.  The start time is defined as either the span start if it exists
// or the timestamp of the first annotation/sub span.
func findStartTime(n *vtrace.Node) time.Time {
	if !n.Span.Start.IsZero() {
		return n.Span.Start
	}
	var startTime time.Time
	for _, a := range n.Span.Annotations {
		startTime = a.When
		if !startTime.IsZero() {
			break
		}
	}
	for _, c := range n.Children {
		childStartTime := findStartTime(c)
		if startTime.IsZero() || (!childStartTime.IsZero() && startTime.After(childStartTime)) {
			startTime = childStartTime
		}
		if !startTime.IsZero() {
			break
		}
	}
	return startTime
}

// findEndTime returns the end time of a node.  The end time is defined as either the span end if it exists
// or the timestamp of the last annotation/sub span.
func findEndTime(n *vtrace.Node) time.Time {
	if !n.Span.End.IsZero() {
		return n.Span.End
	}

	size := len(n.Span.Annotations)
	var endTime time.Time
	for i := range n.Span.Annotations {
		endTime = n.Span.Annotations[size-1-i].When
		if !endTime.IsZero() {
			break
		}
	}

	size = len(n.Children)
	for i := range n.Children {
		childEndTime := findEndTime(n.Children[size-1-i])
		if endTime.IsZero() || (!childEndTime.IsZero() && childEndTime.After(endTime)) {
			endTime = childEndTime
		}
		if !endTime.IsZero() {
			break
		}
	}
	return endTime
}

type vtraceHandler struct{ ctx *context.T }

func (v *vtraceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		server  = r.FormValue("n")
		traceId = r.FormValue("t")
		name    = naming.Join(server, "__debug", "vtrace")
	)
	if len(server) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Must specify a server with the URL query parameter 'n'")
		return
	}

	stub := svtrace.StoreClient(name)
	ctx, tracer := newTracer(v.ctx)
	id, err := uniqueid.FromHexString(traceId)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Invalid trace id %s: %v", traceId, err)
		return
	}

	trace, err := stub.Trace(ctx, id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "Unknown trace id: %s", traceId)
		return
	}

	var buf bytes.Buffer

	vtrace.FormatTrace(&buf, &trace, nil)
	node := vtrace.BuildTree(&trace)

	if node == nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "coud not find root span for trace")
		return
	}

	tree := convertToTree(node, findStartTime(node), findEndTime(node))
	data := struct {
		Id          string
		Root        *divTree
		ServerName  string
		CommandLine string
		DebugTrace  string
		Vtrace      *Tracer
	}{
		Id:          traceId,
		Root:        tree,
		ServerName:  server,
		CommandLine: fmt.Sprintf("debug vtraces %q", name),
		Vtrace:      tracer,
		DebugTrace:  buf.String(),
	}
	executeTemplate(v.ctx, w, r, tmplBrowseVtrace, data)
}

func writeEvent(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, "data: %s\n\n", strings.TrimSpace(data))
}

func writeErrorEvent(w http.ResponseWriter, err error) {
	id := fmt.Sprintf("%v", verror.ErrorID(err))
	writeEvent(w, fmt.Sprintf("ERROR(%v): %v", html.EscapeString(id), html.EscapeString(err.Error())))
}

func makeTemplate(name, content string) *template.Template {
	content = "{{template `.header` .}}" + content + "{{template `.footer` .}}"
	t := template.Must(tmplBrowseHeader.Clone())
	t = template.Must(t.AddParseTree(tmplBrowseFooter.Name(), tmplBrowseFooter.Tree))
	colors := []string{"red", "blue", "green"}
	pos := 0
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
		"nextColor": func() string {
			c := colors[pos]
			pos = (pos + 1) % len(colors)
			return c
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
    </div>
    {{end}}
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

	tmplBrowseProfiles = makeTemplate("profiles", `
<section class="section--center mdl-grid">
  <h5>Profiling</h5>
  <div id="parent" class="mdl-cell mdl-cell--12-col">
  <ul>
  <li>CPU
  <div class="fixed-width">go tool pprof {{.URLPrefix}}/profile?n={{urlquery .ServerName}}</div>
  </li>
  <li><a href="{{.URLPrefix}}/heap?n={{urlquery .ServerName}}&debug=1">Heap</a>
  <div class="fixed-width">go tool pprof {{.URLPrefix}}/heap?n={{urlquery .ServerName}}</div>
  </li>
  <li><a href="{{.URLPrefix}}/block?n={{urlquery .ServerName}}&debug=1">Block</a>
  <div class="fixed-width">go tool pprof {{.URLPrefix}}/block?n={{urlquery .ServerName}}</div>
  </li>
  <li><a href="{{.URLPrefix}}/threadcreate?n={{urlquery .ServerName}}&debug=1">Threadcreate</a>
  <div class="fixed-width">go tool pprof {{.URLPrefix}}/threadcreate?n={{urlquery .ServerName}}</div>
  </li>
  <li>Goroutines:
  <a href="{{.URLPrefix}}/goroutine?n={{urlquery .ServerName}}&debug=1">(compact)</a>
  <a href="{{.URLPrefix}}/goroutine?n={{urlquery .ServerName}}&debug=2">(full)</a>
  </li>
  </ul>
  <div id="parent" class="mdl-cell mdl-cell--12-col">
    <i class="material-icons">info</i>The commands above may not work if the
    remote process isn't written in Go. Support for profiling code in other
    languages is in the wishlist.
  </div>
  </div>
</section>
`)
	tmplBrowseAllTraces = makeTemplate("alltraces", `
<script language='javascript'>
function showIdsForLabel(label) {
    var nodes = document.getElementsByClassName('label-container');
    for (var i = 0; i < nodes.length; i++) {
      var node = nodes[i];
      if (node.dataset.label === label) {
         node.style.display = 'block';
      } else {
        node.style.display = 'none';
      }
    };
}
</script>
<section class="section-center mdl-grid">
  {{$buckets := .Buckets}}
  <p>{{range $label := .SortedLabels}}
  {{$ids := index $buckets $label}}
  {{if len $ids | lt 0}}<a href="javascript:showIdsForLabel('{{$label}}')">{{$label}}</a>{{else}}{{$label}}{{end}}
  {{end}}</p>
</section>
<section class="section-center mdl-grid">
  <h5>Traces</h5>
  {{$name := .ServerName}}
  {{range $l, $ids := .Buckets}}<div data-label="{{$l}}" style='display:none;position:relative' class='mdl-cell mdl-cell--12-col label-container'>
     {{$l}}
     <ul>
       {{range $id := $ids}}<li><a href='/vtrace?n={{$name}}&t={{$id.Id}}'>{{$id.Id}}</a></li>
       {{end}}
     </ul>
   </div>{{end}}
 </section>
`)
	tmplBrowseVtrace = makeTemplate("vtrace", `
{{define ".span"}}
<div style="position:relative;left:{{.Start}}%;width:{{.Width}}%;margin:0px;padding-top:2px;" id="div-{{.Id}}">
  <!-- Root span -->
  <div id="root" title="{{.Name}}" style="position:relative;width:100%;background:{{nextColor}};height:15px;display:block;margin:0px;padding:0px"></div>
  {{range $i, $child := .Children}}
  {{template ".span" $child}}
  {{end}}
</div>
{{end}}
{{define ".collapse-nav"}}
<div id="tree-{{.Id}}" style="position:relative;left:5px">
<div id='root' style="position:relative;height:15px;font:10pt" onclick='javascript:toggleTrace("{{.Id}}")'>{{if len .Children | lt 0}}{{len .Children}}{{end}}</div>
{{range .Children}}
{{template ".collapse-nav" .}}
{{end}}
</div>
{{end}}
<style type="text/css">
.hide-children > :not(#root) {
  display:none;
}
</style>
<script language="javascript">
function toggleTrace(id) {
  var treeRoot = document.getElementById("tree-" + id);
  treeRoot.classList.toggle('hide-children');
  var divRoot = document.getElementById('div-' + id);
  divRoot.classList.toggle('hide-children');
}
</script>
<section class="section--center mdl-grid">
  <h5>Vtrace for {{.Id}}</h5>
  <pre>{{.DebugTrace}}</pre>
  <div class="mdl-cell mdl-cell--12-col">
  <div style="display:flex;flex-direction:row">
  <div style="min-width:10%">
  {{template ".collapse-nav" .Root}}
  </div>
  <div id="parent" style="width:80%">
  {{template ".span" .Root}}
  </div>
  </div>
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
          <a class="mdl-navigation__link" href="/profiles?n={{.ServerName}}">Profiles</a>
          <a class="mdl-navigation__link" href="/vtraces?n={{.ServerName}}">Traces</a>
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
        <a class="mdl-navigation__link" href="/profiles?n={{.ServerName}}">Profiles</a>
        <a class="mdl-navigation__link" href="/vtraces?n={{.ServerName}}">Traces</a>
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

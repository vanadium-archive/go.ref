// package viewer exports a store through an HTTP server, with the following
// features.
//
// URL paths correspond to store paths.  For example, if the URL is
// http://myhost/a/b/c, the store value that is fetched is /a/b/c.
//
// Values can be formatted using html templates (documented in the
// text/templates package).  Templates are named according to the type of the
// value that they format, using a path /templates/<pkgPath>/<typeName>.  For
// example, suppose we are viewing the page for /movies/Inception, and it
// contains a value of type *examples/store/mdb/schema.Movie.  We fetch the
// template /templates/examples/store/mdb/schema/Movie, which must be a string
// in html/template format.  If it exists, the template is compiled and used to
// print the value.  If the template does not exist, the value is formatted in
// raw form.
//
// String values that have a path ending with suffix .css are printed in raw
// form.
package viewer

import (
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"path/filepath"

	"veyron2"
	"veyron2/context"
	"veyron2/naming"
	"veyron2/storage"
	"veyron2/vlog"
)

// server is the HTTP server handler.
type server struct {
	storeRoot string
	store     storage.Store
	runtime   veyron2.Runtime
}

var _ http.Handler = (*server)(nil)

const (
	// rawTemplateText is used to format the output in a raw textual form.
	rawTemplateText = `<!DOCTYPE html>
<html>
{{$entry := .}}
{{$prefix := $entry.Name}}
{{$rawSubdirs := $entry.RawSubdirs}}
<head>
<title>{{.Name}}</title>
</head>
<body>
<h1>{{.Name}}</h1>
<pre>{{.Value}}</pre>
{{with .Subdirs}}
<h3>Subdirectories</h3>
{{range .}}
{{$name := $entry.Join $prefix .}}
<p><a href="{{$name}}{{if $rawSubdirs}}?raw{{end}}">{{.}}</a></p>
{{end}}
{{end}}
</body>
</html>`
)

var (
	rawTemplate = mustParse("raw", rawTemplateText)
)

// mustParse parses the template text.  It panics on error.
func mustParse(name, text string) *template.Template {
	tmpl, err := template.New(name).Parse(text)
	if err != nil {
		panic(fmt.Sprintf("Error parsing template %q: %s", text, err))
	}
	return tmpl
}

// abspath returns the absolute path from a path relative to the store root.
func (s *server) abspath(path string) string {
	return naming.Join(s.storeRoot, path)
}

// loadTemplate fetches the template for the value from the store.  The template
// is based on the type of the value, under /template/<pkgPath>/<typeName>.
func (s *server) loadTemplate(ctx context.T, v interface{}) *template.Template {
	path := templatePath(v)
	en, err := s.store.Bind(s.abspath(path)).Get(ctx)
	if err != nil {
		return nil
	}
	str, ok := en.Value.(string)
	if !ok {
		return nil
	}
	tmpl, err := template.New(path).Parse(str)
	if err != nil {
		vlog.Infof("Template error: %s: %s", path, err)
		return nil
	}
	return tmpl
}

// printRawValuePage prints the value in raw format.
func (s *server) printRawValuePage(ctx context.T, w http.ResponseWriter, path string, v interface{}, rawSubdirs bool) {
	var p printer
	p.print(v)
	x := &EntryForRawTemplate{&Entry{ctx: ctx, storeRoot: s.storeRoot, store: s.store, Name: path, Value: p.String()}, []string{}, rawSubdirs}
	x.Subdirs, _ = x.Glob("*")
	if err := rawTemplate.Execute(w, x); err != nil {
		w.Write([]byte(html.EscapeString(err.Error())))
	}
}

// printValuePage prints the value using a template if possible.  If a template
// is not found, the value is printed in raw format instead.
func (s *server) printValuePage(ctx context.T, w http.ResponseWriter, path string, v interface{}) {
	if tmpl := s.loadTemplate(ctx, v); tmpl != nil {
		x := &Entry{ctx: ctx, storeRoot: s.storeRoot, store: s.store, Name: path, Value: v}
		if err := tmpl.Execute(w, x); err != nil {
			w.Write([]byte(html.EscapeString(err.Error())))
		}
		return
	}
	s.printRawValuePage(ctx, w, path, v, false)
}

// printRawPage prints a string value directly, without processing.
func (s *server) printRawPage(w http.ResponseWriter, v interface{}) {
	str, ok := v.(string)
	if !ok {
		fmt.Fprintf(w, "%s", v)
	} else {
		io.WriteString(w, str)
	}
}

// ServeHTTP is the main HTTP handler.
func (s *server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	ctx := s.runtime.NewContext()
	en, err := s.store.Bind(s.abspath(path)).Get(ctx)
	if err != nil {
		msg := fmt.Sprintf(
			"<html><body><h1>%s</h1><h2>Error: %s</h2></body></html>",
			html.EscapeString(path),
			html.EscapeString(err.Error()))
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(msg))
		return
	}

	q := req.URL.Query()
	switch filepath.Ext(path) {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		s.printRawPage(w, en.Value)
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if q["raw"] != nil {
			s.printRawValuePage(ctx, w, path, en.Value, true)
		} else {
			s.printValuePage(ctx, w, path, en.Value)
		}
	}
}

// ListenAndServe is the main entry point.  It serves store at the specified
// network address.
func ListenAndServe(runtime veyron2.Runtime, addr string, storeRoot string, st storage.Store) error {
	s := &server{storeRoot: storeRoot, store: st, runtime: runtime}
	vlog.Infof("Viewer running at http://localhost%s", addr)
	return http.ListenAndServe(addr, s)
}

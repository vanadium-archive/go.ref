package util

import (
	"html/template"
	"net/http"

	"veyron.io/veyron/veyron2/vlog"
)

// HTTPSend encodes obj using VOM and writes it out to the response in base64
// encoding.
func HTTPSend(w http.ResponseWriter, obj interface{}) {
	b64, err := Base64VomEncode(obj)
	if err != nil {
		HTTPServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte(b64))
	vlog.Infof("Sending %T=%v", obj, obj)
}

// HTTPBadRequest sends an HTTP 400 error on 'w' and renders a pretty page.
// If err is not nil, it also renders the string representation of err in the response page.
func HTTPBadRequest(w http.ResponseWriter, req *http.Request, err error) {
	w.WriteHeader(http.StatusBadRequest)
	if e := tmplBadRequest.Execute(w, badRequestData{Request: requestString(req), Error: err}); e != nil {
		vlog.Errorf("Failed to execute Bad Request Template:", e)
	}
}

// ServerError sends an HTTP 500 error on 'w' and renders a pretty page that
// also has the string representation of err.
func HTTPServerError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	if e := tmplServerError.Execute(w, err); e != nil {
		vlog.Errorf("Failed to execute Server Error template:", e)
	}
}

var (
	tmplBadRequest = template.Must(template.New("Bad Request").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF8">
<title>Bad Request</title>
</head>
<body>
<h1>Bad Request</h1>
{{with $data := .}}
{{if $data.Error}}Error: {{$data.Error}}{{end}}
<pre>
{{$data.Request}}
</pre>
{{end}}
</body>
</html>`))

	tmplServerError = template.Must(template.New("Server Error").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF8">
<title>Server Error</title>
</head>
<body>
<h1>Oops! Error at the server</h1>
Error: {{.}}
<br/>
Ask the server administrator to check the server logs
</body>
</html>`))
)

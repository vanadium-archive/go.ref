package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"

	"veyron/services/identity/googleoauth"
	"veyron2/vlog"
)

func getOAuthAuthorizationCodeFromGoogle(clientID string, blessing <-chan string) (<-chan string, error) {
	// Setup an HTTP server so that OAuth authorization codes can be intercepted.
	// Steps:
	// 1. Generate a state token to be included in the HTTP request
	//    (though, arguably, the random port assignment for the HTTP server is
	//    enough for XSRF protecetion)
	// 2. Setup an HTTP server which will intercept redirect links from the OAuth
	//    flow.
	// 3. Print out the link for the user to click
	// 4. Return the authorization code obtained from the redirect to the "result"
	//    channel.
	var stateBuf [32]byte
	if _, err := rand.Read(stateBuf[:]); err != nil {
		return nil, fmt.Errorf("failed to generate state token for OAuth: %v", err)
	}
	state := base64.URLEncoding.EncodeToString(stateBuf[:])

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to setup OAuth authorization code interception: %v", err)
	}
	redirectURL := fmt.Sprintf("http://%s", ln.Addr())
	result := make(chan string, 1)
	result <- redirectURL
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		tmplArgs := struct {
			Blessing, ErrShort, ErrLong string
		}{}
		defer func() {
			if len(tmplArgs.ErrShort) > 0 {
				w.WriteHeader(http.StatusBadRequest)
			}
			if err := tmpl.Execute(w, tmplArgs); err != nil {
				vlog.Info("Failed to render template:", err)
			}
		}()
		if urlstate := r.FormValue("state"); urlstate != state {
			tmplArgs.ErrShort = "Unexpected request"
			tmplArgs.ErrLong = "Mismatched state parameter. Possible cross-site-request-forging?"
			return
		}
		code := r.FormValue("code")
		if len(code) == 0 {
			tmplArgs.ErrShort = "No authorization code received"
			tmplArgs.ErrLong = "Expected Google to issue an authorization code using 'code' as a URL parameter in the redirect"
			return
		}
		ln.Close() // No need for the HTTP server anymore
		result <- code
		blessed, ok := <-blessing
		defer close(result)
		if !ok {
			tmplArgs.ErrShort = "No blessing received"
			tmplArgs.ErrLong = "Unable to obtain blessing from the Veyron service"
			return
		}
		tmplArgs.Blessing = blessed
		return
	})
	go http.Serve(ln, nil)

	// Print out the link to start the OAuth flow (to STDERR so that STDOUT output contains
	// only the final blessing) and try to open it in the browser as well.
	//
	// TODO(ashankar): Detect devices with limited I/O and then decide to use the device flow
	// instead? See: https://developers.google.com/accounts/docs/OAuth2#device
	url := googleoauth.NewOAuthConfig(clientID, "", redirectURL).AuthCodeURL(state)
	fmt.Fprintln(os.Stderr, "Please visit the following URL to authenticate with Google:")
	fmt.Fprintln(os.Stderr, url)
	// Make an attempt to start the browser as a convenience.
	// If it fails, doesn't matter - the client can see the URL printed above.
	// Use exec.Command().Start instead of exec.Command().Run since there is no
	// need to wait for the command to return (and indeed on some window managers,
	// the command will not exit until the browser is closed).
	exec.Command(openCommand, url).Start()
	return result, nil
}

var tmpl = template.Must(template.New("name").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Veyron Identity: Google</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.2.0/css/bootstrap.min.css">
{{if .Blessing}}
<!--Attempt to close the window. Though this script does not work on many browser configurations-->
<script type="text/javascript">window.close();</script>
{{end}}
</head>
<body>
<div class="container">
{{if .ErrShort}}
<h1><span class="label label-danger">error</span>{{.ErrShort}}</h1>
<div class="well">{{.ErrLong}}</div>
{{else}}
<h3>Received blessing: <tt>{{.Blessing}}</tt></h3>
<div class="well">If the name is prefixed with "unknown/", ignore that. You can close this window, the command line tool has retrieved the blessing</div>
{{end}}
</div>
</body>
</html>`))

package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"v.io/core/veyron/services/identity/oauth"
	"v.io/core/veyron2/vlog"
)

func getMacaroonForBlessRPC(blessServerURL string, blessedChan <-chan string, browser bool) (<-chan string, error) {
	// Setup a HTTP server to recieve a blessing macaroon from the identity server.
	// Steps:
	// 1. Generate a state token to be included in the HTTP request
	//    (though, arguably, the random port assigment for the HTTP server is enough
	//    for XSRF protection)
	// 2. Setup a HTTP server which will receive the final blessing macaroon from the id server.
	// 3. Print out the link (to start the auth flow) for the user to click.
	// 4. Return the macaroon and the rpc object name(where to make the MacaroonBlesser.Bless RPC call)
	//    in the "result" channel.
	var stateBuf [32]byte
	if _, err := rand.Read(stateBuf[:]); err != nil {
		return nil, fmt.Errorf("failed to generate state token for OAuth: %v", err)
	}
	state := base64.URLEncoding.EncodeToString(stateBuf[:])

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to setup authorization code interception server: %v", err)
	}
	result := make(chan string)

	redirectURL := fmt.Sprintf("http://%s/macaroon", ln.Addr())
	http.HandleFunc("/macaroon", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		tmplArgs := struct {
			Blessings, ErrShort, ErrLong string
		}{}
		defer func() {
			if len(tmplArgs.ErrShort) > 0 {
				w.WriteHeader(http.StatusBadRequest)
			}
			if err := tmpl.Execute(w, tmplArgs); err != nil {
				vlog.Info("Failed to render template:", err)
			}
		}()

		toolState := r.FormValue("state")
		if toolState != state {
			tmplArgs.ErrShort = "Unexpected request"
			tmplArgs.ErrLong = "Mismatched state parameter. Possible cross-site-request-forgery?"
			return
		}
		result <- r.FormValue("macaroon")
		result <- r.FormValue("object_name")
		defer close(result)
		blessed, ok := <-blessedChan
		if !ok {
			tmplArgs.ErrShort = "No blessings received"
			tmplArgs.ErrLong = "Unable to obtain blessings from the Veyron service"
			return
		}
		tmplArgs.Blessings = blessed
		ln.Close()
	})
	go http.Serve(ln, nil)

	// Print the link to start the flow.
	url, err := seekBlessingsURL(blessServerURL, redirectURL, state)
	if err != nil {
		return nil, fmt.Errorf("failed to create seekBlessingsURL: %s", err)
	}
	fmt.Fprintln(os.Stdout, "Please visit the following URL to seek blessings:")
	fmt.Fprintln(os.Stdout, url)
	// Make an attempt to start the browser as a convenience.
	// If it fails, doesn't matter - the client can see the URL printed above.
	// Use exec.Command().Start instead of exec.Command().Run since there is no
	// need to wait for the command to return (and indeed on some window managers,
	// the command will not exit until the browser is closed).
	if len(openCommand) != 0 && browser {
		exec.Command(openCommand, url).Start()
	}
	return result, nil
}

func seekBlessingsURL(blessServerURL, redirectURL, state string) (string, error) {
	baseURL, err := url.Parse(joinURL(blessServerURL, oauth.SeekBlessingsRoute))
	if err != nil {
		return "", fmt.Errorf("failed to parse url: %v", err)
	}
	params := url.Values{}
	params.Add("redirect_url", redirectURL)
	params.Add("state", state)
	baseURL.RawQuery = params.Encode()
	return baseURL.String(), nil
}

func joinURL(baseURL, suffix string) string {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return baseURL + suffix
}

var tmpl = template.Must(template.New("name").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Veyron Identity: Google</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.2.0/css/bootstrap.min.css">
{{if .Blessings}}
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
<h3>Received blessings: <tt>{{.Blessings}}</tt></h3>
{{end}}
</div>
</body>
</html>`))

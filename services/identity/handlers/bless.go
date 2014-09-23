package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"veyron.io/veyron/veyron/services/identity/util"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

// Bless is an http.HandlerFunc that renders/processes a form that takes as
// input a base64-vom-encoded security.PrivateID (blessor) and a
// security.PublicID (blessee) and returns a base64-vom-encoded
// security.PublicID obtained by blessor.Bless(blessee, ...).
func Bless(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderForm(w, r)
		return
	}
	if r.Method != "POST" {
		util.HTTPBadRequest(w, r, fmt.Errorf("only GET or POST requests accepted"))
		return
	}
	if err := r.ParseForm(); err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("failed to parse form: %v", err))
		return
	}
	duration, err := time.ParseDuration(defaultIfEmpty(r.FormValue("duration"), "24h"))
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("failed to parse duration: %v", err))
		return
	}
	blessor, err := decodeBlessor(r)
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}
	blessee, err := decodeBlessee(r)
	if err != nil {
		util.HTTPBadRequest(w, r, err)
		return
	}
	name := r.FormValue("name")
	caveats, err := caveats(r)
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("failed to created caveats: ", err))
	}
	blessed, err := blessor.Bless(blessee, name, duration, caveats)
	if err != nil {
		util.HTTPBadRequest(w, r, fmt.Errorf("%q.Bless(%q, %q) failed: %v", blessor, blessee, name, err))
		return
	}
	util.HTTPSend(w, blessed)
}

func caveats(r *http.Request) ([]security.Caveat, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	var caveats []security.Caveat
	for i, cavName := range r.Form["caveat"] {
		if cavName == "none" {
			continue
		}
		args := strings.Split(r.Form[cavName][i], ",")
		cavInfo, ok := caveatMap[cavName]
		if !ok {
			return nil, fmt.Errorf("unable to create caveat %s: caveat does not exist", cavName)
		}
		caveat, err := cavInfo.New(args...)
		if err != nil {
			return nil, fmt.Errorf("unable to create caveat %s(%v): cavInfo.New failed: %v", cavName, args, err)
		}
		caveats = append(caveats, caveat)
	}
	return caveats, nil
}

func decodeBlessor(r *http.Request) (security.PrivateID, error) {
	var blessor security.PrivateID
	b64 := r.FormValue("blessor")
	if err := util.Base64VomDecode(b64, &blessor); err != nil {
		return nil, fmt.Errorf("Base64VomDecode of blessor into %T failed: %v", blessor, err)
	}
	return blessor, nil
}

func decodeBlessee(r *http.Request) (security.PublicID, error) {
	var pub security.PublicID
	b64 := r.FormValue("blessee")
	if err := util.Base64VomDecode(b64, &pub); err == nil {
		return pub, nil
	}
	var priv security.PrivateID
	err := util.Base64VomDecode(b64, &priv)
	if err == nil {
		return priv.PublicID(), nil
	}
	return nil, fmt.Errorf("Base64VomDecode of blessee into %T or %T failed: %v", pub, priv, err)
}

func renderForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Context-Type", "text/html")
	if err := tmpl.Execute(w, caveatMap); err != nil {
		vlog.Errorf("Unable to execute bless page template: %v", err)
		util.HTTPServerError(w, err)
	}
}

var tmpl = template.Must(template.New("bless").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Veyron Identity Derivation</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.0.0/css/bootstrap.min.css">
<script src="//ajax.googleapis.com/ajax/libs/jquery/1.11.1/jquery.min.js"></script>
<script>
	// TODO(suharshs): Move this and other JS/CSS to an assets directory in identity server.
	$(document).ready(function() {
		$('.caveatInput').hide(); // Hide all the inputs at start.

		// When a caveat selector changes show the corresponding input box.
		$('body').on('change', '.caveats', function (){
			// Grab the div encapsulating the select and the corresponding inputs.
			var caveatSelector = $(this).parents(".caveatRow");
			// Hide the visible inputs and show the selected one.
			caveatSelector.find('.caveatInput').hide();
			caveatSelector.find('#'+$(this).val()).show();
		});

		// Upon clicking the '+' button a new caveat selector should appear.
		$('body').on('click', '.addCaveat', function() {
			var selector = $(this).parents(".caveatRow");
			var newSelector = selector.clone();
			// Hide all inputs since nothing is selected in this clone.
			newSelector.find('.caveatInput').hide();
			selector.after(newSelector);
			// Change the '+' button to a '-' button.
			$(this).replaceWith('<button type="button" class="btn btn-danger btn-sm removeCaveat">-</button>')
		});

		// Upon clicking the '-' button caveats should be removed.
		$('body').on('click', '.removeCaveat', function() {
			$(this).parents(".caveatRow").remove();
		});
	});
</script>
</head>
<body class="container">
<form class="form-signin" method="POST" name="input" action="/bless/">
<h2 class="form-signin-heading">Blessings</h2>
<input type="text" class="form-control" name="blessor" placeholder="Base64VOM encoded PrivateID of blessor">
<br/>
<input type="text" class="form-control" name="blessee" placeholder="Base64VOM encoded PublicID/PrivateID of blessee">
<br/>
<input type="text" class="form-control" name="name" placeholder="Name">
<br/>
<input type="text" class="form-control" name="duration" placeholder="Duration. Defaults to 24h">
<div class="caveatRow row">
	<br/>
	<div class="col-md-4">
		<select name="caveat" class="form-control caveats">
		  <option value="none" selected="selected">Select a caveat.</option>
		  {{ $caveatMap := . }}
		  {{range $key, $value := $caveatMap}}
		  <option name="{{$key}}" value="{{$key}}">{{$key}}</option>
		  {{end}}
		</select>
	</div>
	<div class="col-md-7">
	  {{range $key, $entry := $caveatMap}}
	  	<input type="text" id="{{$key}}" class="form-control caveatInput" name="{{$key}}" placeholder="{{$entry.Placeholder}}">
	  {{end}}
	</div>
	<div class="col-md-1">
		<button type="button" class="btn btn-info btn-sm addCaveat">+</button>
	</div>
</div>
<br/>
<button class="btn btn-lg btn-primary btn-block" type="submit">Bless</button>
</form>
</body>
</html>`))

func defaultIfEmpty(str, def string) string {
	if len(str) == 0 {
		return def
	}
	return str
}

// caveatMap is a map from Caveat name to caveat information.
// To add to this map append
// key = "CaveatName"
// New = func that returns instantiation of specific caveat wrapped in security.Caveat.
// Placeholder = the placeholder text for the html input element.
var caveatMap = map[string]struct {
	New         func(args ...string) (security.Caveat, error)
	Placeholder string
}{
	"ExpiryCaveat": {
		New: func(args ...string) (security.Caveat, error) {
			if len(args) != 1 {
				return security.Caveat{}, fmt.Errorf("must pass exactly one duration string.")
			}
			dur, err := time.ParseDuration(args[0])
			if err != nil {
				return security.Caveat{}, fmt.Errorf("parse duration failed: %v", err)
			}
			return security.ExpiryCaveat(time.Now().Add(dur))
		},
		Placeholder: "i.e. 2h45m. Valid time units are ns, us (or µs), ms, s, m, h.",
	},
	"MethodCaveat": {
		New: func(args ...string) (security.Caveat, error) {
			if len(args) < 1 {
				return security.Caveat{}, fmt.Errorf("must pass at least one method")
			}
			return security.MethodCaveat(args[0], args[1:]...)
		},
		Placeholder: "Comma-separated method names.",
	},
	"PeerBlessingsCaveat": {
		New: func(args ...string) (security.Caveat, error) {
			if len(args) < 1 {
				return security.Caveat{}, fmt.Errorf("must pass at least one blessing pattern")
			}
			var patterns []security.BlessingPattern
			for _, arg := range args {
				patterns = append(patterns, security.BlessingPattern(arg))
			}
			return security.PeerBlessingsCaveat(patterns[0], patterns[1:]...)
		},
		Placeholder: "Comma-separated blessing patterns.",
	},
}

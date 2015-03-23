package caveats

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"v.io/v23/security"
)

type browserCaveatSelector struct{}

// NewBrowserCaveatSelector returns a caveat selector that renders a form in the
// to accept user caveat selections.
func NewBrowserCaveatSelector() CaveatSelector {
	return &browserCaveatSelector{}
}

func (s *browserCaveatSelector) Render(blessingExtension, state, redirectURL string, w http.ResponseWriter, r *http.Request) error {
	tmplargs := struct {
		Extension             string
		CaveatList            []string
		Macaroon, MacaroonURL string
	}{blessingExtension, []string{"ExpiryCaveat", "MethodCaveat", "PeerBlessingsCaveat"}, state, redirectURL}
	w.Header().Set("Context-Type", "text/html")
	if err := tmplSelectCaveats.Execute(w, tmplargs); err != nil {
		return err
	}
	return nil
}

func (s *browserCaveatSelector) ParseSelections(r *http.Request) (caveats []CaveatInfo, state string, additionalExtension string, err error) {
	if caveats, err = s.caveats(r); err != nil {
		return
	}
	state = r.FormValue("macaroon")
	additionalExtension = r.FormValue("blessingExtension")
	return
}

func (s *browserCaveatSelector) caveats(r *http.Request) ([]CaveatInfo, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	var caveats []CaveatInfo
	// Fill in the required caveat.
	switch required := r.FormValue("requiredCaveat"); required {
	case "Expiry":
		expiry, err := newExpiryCaveatInfo(r.FormValue("expiry"), r.FormValue("timezoneOffset"))
		if err != nil {
			return nil, fmt.Errorf("failed to create ExpiryCaveat: %v", err)
		}
		caveats = append(caveats, expiry)
	case "Revocation":
		revocation := newRevocationCaveatInfo()
		caveats = append(caveats, revocation)
	default:
		return nil, fmt.Errorf("%q is not a valid required caveat", required)
	}
	if len(caveats) != 1 {
		return nil, fmt.Errorf("server does not allow for un-restricted blessings")
	}

	// And find any additional ones
	for i, cavName := range r.Form["caveat"] {
		var err error
		var caveat CaveatInfo
		switch cavName {
		case "ExpiryCaveat":
			caveat, err = newExpiryCaveatInfo(r.Form[cavName][i], r.FormValue("timezoneOffset"))
		case "MethodCaveat":
			caveat, err = newMethodCaveatInfo(r.Form[cavName][i])
		case "PeerBlessingsCaveat":
			caveat, err = newPeerBlessingsCaveatInfo(r.Form[cavName][i])
		case "none":
			continue
		default:
			err = errors.New("caveat does not exist")
		}
		if err != nil {
			return nil, fmt.Errorf("unable to create caveat %s: %v", cavName, err)
		}
		caveats = append(caveats, caveat)
	}
	return caveats, nil
}

func newExpiryCaveatInfo(timestamp, utcOffset string) (CaveatInfo, error) {
	var empty CaveatInfo
	t, err := time.Parse("2006-01-02T15:04", timestamp)
	if err != nil {
		return empty, fmt.Errorf("parseTime failed: %v", err)
	}
	// utcOffset is returned as minutes from JS, so we need to parse it to a duration.
	offset, err := time.ParseDuration(utcOffset + "m")
	if err != nil {
		return empty, fmt.Errorf("failed to parse duration: %v", err)
	}
	return CaveatInfo{"Expiry", []interface{}{t.Add(offset)}}, nil
}

func newMethodCaveatInfo(methodsCSV string) (CaveatInfo, error) {
	methods := strings.Split(methodsCSV, ",")
	if len(methods) < 1 {
		return CaveatInfo{}, fmt.Errorf("must pass at least one method")
	}
	var ifaces []interface{}
	for _, m := range methods {
		ifaces = append(ifaces, m)
	}
	return CaveatInfo{"Method", ifaces}, nil
}

func newPeerBlessingsCaveatInfo(patternsCSV string) (CaveatInfo, error) {
	patterns := strings.Split(patternsCSV, ",")
	if len(patterns) < 1 {
		return CaveatInfo{}, fmt.Errorf("must pass at least one peer blessing pattern")
	}
	var ifaces []interface{}
	for _, p := range patterns {
		ifaces = append(ifaces, security.BlessingPattern(p))
	}
	return CaveatInfo{"PeerBlessings", ifaces}, nil
}

func newRevocationCaveatInfo() CaveatInfo {
	return CaveatInfo{Type: "Revocation"}
}

var tmplSelectCaveats = template.Must(template.New("bless").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Blessings: Select caveats</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.0.0/css/bootstrap.min.css">
<link rel="stylesheet" href="//cdnjs.cloudflare.com/ajax/libs/toastr.js/latest/css/toastr.min.css">
<script src="//cdnjs.cloudflare.com/ajax/libs/moment.js/2.7.0/moment.min.js"></script>
<script src="//ajax.googleapis.com/ajax/libs/jquery/1.11.1/jquery.min.js"></script>
<script src="//cdnjs.cloudflare.com/ajax/libs/toastr.js/latest/js/toastr.min.js"></script>
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
      $(this).parents('.caveatRow').remove();
    });

    // Get the timezoneOffset for the server to create a correct expiry caveat.
    // The offset is the minutes between UTC and local time.
    var d = new Date();
    $('#timezoneOffset').val(d.getTimezoneOffset());

    // Set the datetime picker to have a default value of one day from now.
    var m = moment().add(1, 'd').format("YYYY-MM-DDTHH:MM")
    $('#expiry').val(m);
    $('#ExpiryCaveat').val(m);
  });
</script>
</head>
<body class="container">
<form class="form-horizontal" method="POST" id="caveats-form" name="input" action="{{.MacaroonURL}}" role="form">
<h2 class="form-signin-heading">{{.Extension}}</h2>
<input type="text" class="hidden" name="macaroon" value="{{.Macaroon}}">
<div class="form-group form-group-lg">
  <label class="col-sm-2" for="blessing-extension">Extension</label>
  <div class="col-sm-10">
  <input name="blessingExtension" type="text" class="form-control" id="blessing-extension" placeholder="(optional) name of the device/application for which the blessing is being sought, e.g. homelaptop">
  <input type="text" class="hidden" id="timezoneOffset" name="timezoneOffset">
  </div>
</div>
<div class="form-group form-group-lg">
  <label class="col-sm-2" for="required-caveat">Expiration</label>
  <div class="col-sm-10" class="input-group" name="required-caveat">
    <div class="radio">
      <label>
      <input type="radio" name="requiredCaveat" id="requiredCaveat" value="Revocation" checked>
      When explicitly revoked
      </label>
    </div>
    <div class="radio">
      <div class="input-group">
        <input type="radio" name="requiredCaveat" id="requiredCaveat" value="Expiry">
        <input type="datetime-local" id="expiry" name="expiry">
      </div>
    </div>
  </div>
</div>
<h4 class="form-signin-heading">Additional caveats</h4>
<span class="help-text">Optional additional restrictions on the use of the blessing</span>
<div class="caveatRow row">
  <div class="col-md-4">
    <select name="caveat" class="form-control caveats">
      <option value="none" selected="selected">Select a caveat.</option>
      {{ $caveatList := .CaveatList }}
      {{range $index, $name := $caveatList}}
      <option name="{{$name}}" value="{{$name}}">{{$name}}</option>
      {{end}}
    </select>
  </div>
  <div class="col-md-7">
    {{range $index, $name := $caveatList}}
      {{if eq $name "ExpiryCaveat"}}
      <input type="datetime-local" class="form-control caveatInput" id="{{$name}}" name="{{$name}}">
      {{else if eq $name "MethodCaveat"}}
      <input type="text" id="{{$name}}" class="form-control caveatInput" name="{{$name}}" placeholder="comma-separated method list">
      {{else if eq $name "PeerBlessingsCaveat"}}
      <input type="text" id="{{$name}}" class="form-control caveatInput" name="{{$name}}" placeholder="comma-separated blessing-pattern list">
      {{end}}
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

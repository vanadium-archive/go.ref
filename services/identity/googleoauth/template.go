package googleoauth

import (
	"crypto/ecdsa"
	"crypto/md5"
	"crypto/x509"
	"html/template"
)

// TODO(suharshs): Add an if statement to only show the revoke buttons for non-revoked ids.
var tmpl = template.Must(template.New("auditor").Funcs(tmplFuncMap()).Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Blessings for {{.Email}}</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.2.0/css/bootstrap.min.css">
<script src="//cdnjs.cloudflare.com/ajax/libs/moment.js/2.7.0/moment.min.js"></script>
<script src="//ajax.googleapis.com/ajax/libs/jquery/1.11.1/jquery.min.js"></script>
<script src="//ajax.googleapis.com/ajax/libs/jqueryui/1.11.0/jquery-ui.min.js"></script>
<script>
function setTimeText(elem) {
  var timestamp = elem.data("unixtime");
  var m = moment(timestamp*1000.0);
  var style = elem.data("style");
  if (style === "absolute") {
    elem.html("<a href='#'>" + m.format("dd, MMM Do YYYY, h:mm:ss a") + "</a>");
    elem.data("style", "fromNow");
  } else {
    elem.html("<a href='#'>" + m.fromNow() + "</a>");
    elem.data("style", "absolute");
  }
}

$(document).ready(function() {
  $(".unixtime").each(function() {
    // clicking the timestamp should toggle the display format.
    $(this).click(function() { setTimeText($(this)); });
    setTimeText($(this));
  });

  // Setup the revoke buttons click events.
  $(".revoke").click(function() {
    var revokeButton = $(this);
    $.ajax({
      url: "/google/revoke",
      type: "POST",
      data: JSON.stringify({
        "CaveatID": revokeButton.val(),
        "CSRFToken": "{{.CSRFToken}}"
      })
    }).done(function(data) {
      // TODO(suharshs): Have a fail message, add a strikethrough on the revoked caveats.
      console.log(data)
      revokeButton.remove()
    }).fail(function(jqXHR, textStatus){
      console.log(jqXHR)
      console.log("The request failed :( :", textStatus)
    });
  });
});

</script>
</head>
<body>
<div class="container">
<h3>Blessing log for {{.Email}}</h3>
<table class="table table-bordered table-hover table-responsive">
<thead>
<tr>
  <th>Blessing sought as</th>
  <th>Blessed as</th>
  <th>Issued</th>
  <th>Expires</th>
  <th>PublicKey</th>
  <th>Revoked</th>
  </tr>
</thead>
<tbody>
{{range .Log}}
<tr>
<td>{{.Blessee}}</td>
<td>{{.Blessed}}</td>
<td><div class="unixtime" data-unixtime={{.Start.Unix}}>{{.Start.String}}</div></td>
<td><div class="unixtime" data-unixtime={{.End.Unix}}>{{.End.String}}</div></td>
<td>{{publicKeyHash .Blessee.PublicKey}}</td>
<td><button class="revoke" value="{{.RevocationCaveatID}}" type="button">Revoke</button></td>
</tr>
{{else}}
<tr>
<td colspan=5>No blessings issued</td>
</tr>
{{end}}
</tbody>
</table>
<hr/>
</div>
</body>
</html>`))

func tmplFuncMap() template.FuncMap {
	m := make(template.FuncMap)
	m["publicKeyHash"] = publicKeyHash
	return m
}

// publicKeyHash returns a human-readable representation of a public key.
// The returned representation is similar to what SSH uses when prompting
// about new hosts (it's the hash of the key).
//
// TODO(ashankar): Might be nice for this representation to be used in other
// places like the "identity" command line tool.
func publicKeyHash(key *ecdsa.PublicKey) string {
	const hextable = "0123456789abcdef"
	bytes, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return err.Error()
	}
	hash := md5.Sum(bytes)
	var repr [md5.Size * 3]byte
	for i, v := range hash {
		repr[i*3] = hextable[v>>4]
		repr[i*3+1] = hextable[v&0x0f]
		repr[i*3+2] = ':'
	}
	return string(repr[:len(repr)-1])
}

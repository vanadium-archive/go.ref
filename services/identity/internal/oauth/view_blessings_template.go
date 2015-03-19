package oauth

import "html/template"

var tmplViewBlessings = template.Must(template.New("auditor").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Blessings for {{.Email}}</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="//netdna.bootstrapcdn.com/bootstrap/3.2.0/css/bootstrap.min.css">
<link rel="stylesheet" href="//cdnjs.cloudflare.com/ajax/libs/toastr.js/latest/css/toastr.min.css">
<script src="//cdnjs.cloudflare.com/ajax/libs/moment.js/2.7.0/moment.min.js"></script>
<script src="//ajax.googleapis.com/ajax/libs/jquery/1.11.1/jquery.min.js"></script>
<script src="//ajax.googleapis.com/ajax/libs/jqueryui/1.11.0/jquery-ui.min.js"></script>
<script src="//cdnjs.cloudflare.com/ajax/libs/toastr.js/latest/js/toastr.min.js"></script>
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
      url: "/google/{{.RevokeRoute}}",
      type: "POST",
      data: JSON.stringify({
        "Token": revokeButton.val()
      })
    }).done(function(data) {
      if (data.success == "false") {
        failMessage(revokeButton);
        return;
      }
      revokeButton.replaceWith("<div>Just Revoked!</div>");
    }).fail(function(xhr, textStatus){
      failMessage(revokeButton);
      console.error('Bad request: %s', status, xhr)
    });
  });
});

function failMessage(revokeButton) {
  revokeButton.parent().parent().fadeIn(function(){
    $(this).addClass("bg-danger");
  });
  toastr.options.closeButton = true;
  toastr.error('Unable to revoke identity!', 'Error!')
}

</script>
</head>
<body>
<div class="container">
<h3>Blessing log for {{.Email}}</h3>
<table class="table table-bordered table-hover table-responsive">
<thead>
  <tr>
  <th>Blessed as</th>
  <th>Public Key</th>
  <th>Issued</th>
  <th>Caveats</th>
  <th>Revoked</th>
  </tr>
</thead>
<tbody>
{{range .Log}}
  {{if .Error}}
    <tr class="bg-danger">
      <td colspan="5">Failed to read audit log: Error: {{.Error}}</td>
    </tr>
  {{else}}
    <tr>
    <td>{{.Blessed}}</td>
    <td>{{.Blessed.PublicKey}}</td>
    <td><div class="unixtime" data-unixtime={{.Timestamp.Unix}}>{{.Timestamp.String}}</div></td>
    <td>
    {{range .Caveats}}
      {{.}}</br>
    {{end}}
    </td>
    <td>
      {{ if .Token }}
      <button class="revoke" value="{{.Token}}">Revoke</button>
      {{ else if not .RevocationTime.IsZero }}
        <div class="unixtime" data-unixtime={{.RevocationTime.Unix}}>{{.RevocationTime.String}}</div>
      {{ end }}
    </td>
    </tr>
  {{end}}
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

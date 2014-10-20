package googleoauth

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
<td>{{.Blessee.PublicKey}}</td>
<td>
  {{ if .Token }}
  <button class="revoke" value="{{.Token}}">Revoke</button>
  {{ else if not .RevocationTime.IsZero }}
    <div class="unixtime" data-unixtime={{.RevocationTime.Unix}}>{{.RevocationTime.String}}</div>
  {{ end }}
</td>
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

var tmplSelectCaveats = template.Must(template.New("bless").Parse(`<!doctype html>
<html>
<head>
<meta charset="UTF-8">
<title>Blessings: Select caveats</title>
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
<form class="form-signin" method="POST" name="input" action="/google/{{.MacaroonRoute}}">
<h2 class="form-signin-heading">{{.Extension}}</h2>
<input type="text" class="hidden" name="macaroon" value="{{.Macaroon}}">
<div class="form-group">
  <label for="blessing-extension">Extension</label>
  <input name="blessingExtension" type="text" class="form-control" id="blessing-extension" placeholder="(optional) If set to foo, then blessings will be of the form {{.Extension}}/foo">
</div>
<br/>
<h3 class="form-signin-heading">Select Caveats</h3>
<div class="caveatRow row">
<br/>
  <div class="col-md-4">
    <select name="caveat" class="form-control caveats">
      <option value="none" selected="selected">Select a caveat.</option>
      {{ $caveatMap := .CaveatMap }}
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

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package templates

import "html/template"

var ListBlessings = template.Must(listBlessings.Parse(headPartial))

var listBlessings = template.Must(template.New("auditor").Parse(`<!doctype html>
<html>
<head>
  {{template "head" .}}
  <title>Blessings for {{.Email}}</title>
  <link rel="stylesheet" href="{{.AssetsPrefix}}/identity/toastr.css">
  <script src="{{.AssetsPrefix}}/identity/toastr.js"></script>
  <script src="{{.AssetsPrefix}}/identity/moment.js"></script>
  <script src="{{.AssetsPrefix}}/identity/jquery.js"></script>

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
        url: "/auth/google/{{.RevokeRoute}}",
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

<body class="default-layout">
  <header>
    <nav class="left">
      <a href="#" class="logo">Vanadium</a>
    </nav>

    <nav class="main">
      <a href="#">Blessing Log</a>
    </nav>

    <nav class="right">
      <a href="#">{{.Email}}</a>
    </nav>
  </header>

  <main style="margin-left: 0px; max-width: 100%;">

    <!-- Begin ID Server information -->
    <div class="grid">
      <div class="cell">
        <h2>Public Key</h2>
        <p>
          The public key of this provider is <code>{{.Self.PublicKey}}</code>.</br>
          The root names and public key (in DER encoded <a href="http://en.wikipedia.org/wiki/X.690#DER_encoding">format</a>)
          are available in a <a class="btn btn-xs btn-primary" href="/auth/blessing-root">JSON</a> object.
        </p>
      </div>
      {{if .GoogleServers}}
      <div class="cell">
        <h2>Blessings</h2>
        <p>
          Blessings (using Google OAuth to fetch an email address) are provided via
          Vanadium RPCs to: <code>{{range .GoogleServers}}{{.}}{{end}}</code>
        </p>
      </div>
      {{end}}
      {{if .DischargeServers}}
      <div class="cell">
        <h2>Discharges</h2>
        <p>
          RevocationCaveat Discharges are provided via Vanadium RPCs to:
          <code>{{range .DischargeServers}}{{.}}{{end}}</code>
        </p>
      </div>
      {{end}}
    </div>
    <!-- End ID Server information -->

    <table class="blessing-table">
        <tr>
        <td>Blessed as</td>
        <td>Public Key</td>
        <td>Issued</td>
        <td class="td-wide">Caveats</td>
        <td>Revoked</td>
        </tr>
        {{range .Log}}
          {{if .Error}}
            <tr class="">
              <td colspan="5">Failed to read audit log: Error: {{.Error}}</td>
            </tr>
          {{else}}
            <tr>
            <td>{{.Blessed}}</td>
            <td>{{.Blessed.PublicKey}}</td>
            <td><div class="unixtime" data-unixtime={{.Timestamp.Unix}}>{{.Timestamp.String}}</div></td>
            <td class="td-wide">
            {{range $index, $cav := .Caveats}}
              {{if ne $index 0}}
                <hr>
              {{end}}
              {{$cav}}</br>
            {{end}}
            </td>
            <td>
              {{ if .Token }}
              <button class="revoke button-passive" value="{{.Token}}">Revoke</button>
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
    </table>
  </main>
</body>
</html>`))

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package templates

import "html/template"

var SelectCaveats = template.Must(selectCaveats.Parse(headPartial))

var selectCaveats = template.Must(template.New("bless").Parse(`<!doctype html>
<html>
<head>
  {{template "head" .}}
  <script src="//cdnjs.cloudflare.com/ajax/libs/moment.js/2.7.0/moment.min.js"></script>
  <script src="//ajax.googleapis.com/ajax/libs/jquery/1.11.1/jquery.min.js"></script>

  <title>Blessings: Select Caveats</title>
  <script>
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

    // Upon clicking the 'Add Caveat' button a new caveat selector should appear.
    $('body').on('click', '.addCaveat', function() {
      var selector = $(this).parents(".caveatRow");
      var newSelector = selector.clone();
      // Hide all inputs since nothing is selected in this clone.
      newSelector.find('.caveatInput').hide();
      selector.after(newSelector);
      // Change the '+' button to a 'Remove Caveat' button.
      $(this).replaceWith('<button type="button" class="button-passive right removeCaveat">Remove Caveat</button>');
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
    var m = moment().add(1, 'd').format("YYYY-MM-DDTHH:mm")
    $('#expiry').val(m);
    $('#ExpiryCaveat').val(m);

    // Activate the cancel button.
    $('#cancel').click(function() {
      window.close();
    });

    $('#blessing-extension').on('input', function(){
      var ext = $(this).val();
      // If the user has specified an extension, we want to add a leading slash
      // and display the full blessing name to the user.
      if (ext.length > 0) {
        ext = '/' + ext;
      }
      $('.extension-display').text(ext);
    });
  });
  </script>
</head>

<body class="default-layout">

<header>
  <nav class="left">
    <a href="#" class="logo">Vanadium</a>
  </nav>

  <nav class="main">
    <a href="#">Select Caveats</a>
  </nav>

  <nav class="right">
    <a href="#">{{.Extension}}</a>
  </nav>
</header>

<main style="max-width: 80%; margin-left: 10px;">
  <form method="POST" id="caveats-form" name="input" action="{{.MacaroonURL}}" role="form">
  <h3>Seeking Blessing: {{.BlessingName}}/{{.Extension}}<span class="extension-display"></span></h3>
  <input type="text" class="hidden" name="macaroon" value="{{.Macaroon}}">
  <div class="grid">
    <div class="cell">
      <label for="blessing-extension">Extension</label>
      <input name="blessingExtension" type="text" id="blessing-extension" placeholder="(optional) name of the device/application for which the blessing is being sought, e.g. homelaptop">
      <input type="text" class="hidden" id="timezoneOffset" name="timezoneOffset">
    </div>
  </div>
  <div>
    <label for="required-caveat">Expiration</label>
    <div name="required-caveat">
      <div>
        <label>
        <input type="radio" name="requiredCaveat" id="requiredCaveat" value="Revocation" checked>
        When explicitly revoked
        </label>
      </div>
      <div>
        <div>
          <input type="radio" name="requiredCaveat" id="requiredCaveat" value="Expiry">
          <input type="datetime-local" id="expiry" name="expiry">
        </div>
      </div>
    </div>
  </div>
  <h4>Additional caveats</h4>
  <span>Optional additional restrictions on the use of the blessing</span>
  <div class="grid caveatRow">
    <div class="cell">
      <select name="caveat" class="caveats">
        <option value="none" selected="selected">Select a caveat.</option>
        {{ $caveatList := .CaveatList }}
        {{range $index, $name := $caveatList}}
          {{if eq $name "ExpiryCaveat"}}
          <option name="{{$name}}" value="{{$name}}">Expires</option>
          {{else if eq $name "MethodCaveat"}}
          <option name="{{$name}}" value="{{$name}}">Allowed Methods</option>
          {{else if eq $name "PeerBlessingsCaveat"}}
          <option name="{{$name}}" value="{{$name}}">Allowed Peers</option>
          {{else}}
          <option name="{{$name}}" value="{{$name}}">{{$name}}</option>
          {{end}}
        {{end}}
      </select>

      {{range $index, $name := $caveatList}}
        {{if eq $name "ExpiryCaveat"}}
        <input type="datetime-local" class="caveatInput" id="{{$name}}" name="{{$name}}">
        {{else if eq $name "MethodCaveat"}}
        <input type="text" id="{{$name}}" class="caveatInput" name="{{$name}}" placeholder="comma-separated method list">
        {{else if eq $name "PeerBlessingsCaveat"}}
        <input type="text" id="{{$name}}" class="form-control caveatInput" name="{{$name}}" placeholder="comma-separated blessing-pattern list">
        {{end}}
      {{end}}
      <button type="button" class="button-passive right addCaveat">Add Caveat</button>
    </div>
  </div>
  <br/>
  <div>
  The blessing name will be visible to any peers that this blessing is shared
with. Thus, if your email address is in the blessing name, it will be visible
to peers you share the blessing with.
  </div>
  <br>
  <div>
  By clicking "Bless", you consent to be bound by Google's general <a href="https://www.google.com/intl/en/policies/terms/">Terms of Service</a>
  and Google's general <a href="https://www.google.com/intl/en/policies/privacy/">Privacy Policy</a>.
  </div>
  <div class="grid">
    <button class="cell button-passive" type="submit">Bless</button>
    <button class="cell button-passive" id="cancel">Cancel</button>
    <div class="cell"></div>
    <div class="cell"></div>
  </div>
  </form>
</main>

</body>
</html>`))

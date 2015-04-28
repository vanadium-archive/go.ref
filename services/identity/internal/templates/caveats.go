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

  <title>Add Blessing</title>
  <script>
  $(document).ready(function() {
    var numCaveats = 1;
    // When a caveat selector changes show the corresponding input box.
    $('body').on('change', '.caveats', function (){
      var caveatSelector = $(this).parents('.caveatRow');

      // Hide the visible inputs and show the selected one.
      caveatSelector.find('.caveatInput').hide();
      var caveatName = $(this).val();
      if (caveatName !== 'RevocationCaveat') {
        caveatSelector.find('#'+caveatName).show();
      }
    });

    var updateNewSelector = function(newSelector, caveatName) {
      // disable the option from being selected again and make the next caveat the
      // default for the next selector.
      var selectedOption = newSelector.find('option[name="' + caveatName + '"]');
      selectedOption.prop('disabled', true);
      var newCaveat = newSelector.find('option:enabled').first();
      newCaveat.prop('selected', true);
      newSelector.find('.caveatInput').hide();
      newSelector.find('#'+newCaveat.attr('name')).show();
    }

    // Upon clicking the 'Add Caveat' button a new caveat selector should appear.
    $('body').on('click', '.addCaveat', function() {
      var selector = $(this).parents('.caveatRow');
      var newSelector = selector.clone();
      var caveatName = selector.find('.caveats').val();

      updateNewSelector(newSelector, caveatName);

      // Change the selector's select to a fixed label and fix the inputs.
      selector.find('.caveats').hide();
      selector.find('.'+caveatName+'Selected').show();
      selector.find('.caveatInput').prop('readonly', true);

      selector.after(newSelector);
      $(this).replaceWith('<button type="button" class="button-passive right removeCaveat hidden">Remove</button>');

      numCaveats += 1;
      if (numCaveats > 1) {
        $('.removeCaveat').show();
      }
      if (numCaveats >= 4) {
        $('.addCaveat').hide();
        $('.caveats').hide();
      }
    });

    // If add more is selected, remove the button and show the caveats selector.
    $('body').on('click', '.addMore', function() {
      var selector = $(this).parents('.caveatRow');
      var newSelector = selector.clone();
      var caveatName = selector.find('.caveats').val();

      updateNewSelector(newSelector, caveatName);

      newSelector.find('.caveats').show();
      // Change the 'Add more' button in the copied selector to an 'Add caveats' button.
      newSelector.find('.addMore').replaceWith('<button type="button" class="button-primary right addCaveat">Add</button>');
      // Hide the default selected caveat for the copied selector.
      newSelector.find('.selected').hide();

      selector.after(newSelector);
      $(this).replaceWith('<button type="button" class="button-passive right removeCaveat hidden">Remove</button>');
    });

    // Upon clicking submit, caveats that have not been added yet should be removed,
    // before they are sent to the server.
    $('#caveats-form').submit(function(){
      $('.addCaveat').parents('.caveatRow').remove();
      return true;
    });

    // Upon clicking the 'Remove Caveat' button, the caveat row should be removed.
    $('body').on('click', '.removeCaveat', function() {
      var selector = $(this).parents('.caveatRow')
      var caveatName = selector.find('.caveats').val();

      // Enable choosing this caveat again.
      $('option[name="' + caveatName + '"]').last().prop('disabled', false);

      selector.remove();

      numCaveats -= 1;
      if (numCaveats == 1) {
        $('.removeCaveat').hide();
      }
      $('.addCaveat').show();
      $('.caveats').last().show();
    });

    // Get the timezoneOffset for the server to create a correct expiry caveat.
    // The offset is the minutes between UTC and local time.
    var d = new Date();
    $('#timezoneOffset').val(d.getTimezoneOffset());

    // Set the datetime picker to have a default value of one day from now.
    $('.expiry').val(moment().add(1, 'd').format('YYYY-MM-DDTHH:mm'));

    // Activate the cancel button.
    $('#cancel').click(window.close);
  });
  </script>
</head>

<body class="default-layout">

<header>
  <nav class="left">
    <a href="#" class="logo">Vanadium</a>
  </nav>
  <nav class="right">
    <a href="#">{{.Extension}}</a>
  </nav>
</header>

<main style="max-width: 80%; margin-left: 10px;">
  <form method="POST" id="caveats-form" name="input" action="{{.MacaroonURL}}" role="form">
  <h1>Add blessing</h1>
  <span>
  This is a beta product: use in production applications is discouraged. Furthermore, the
  <a href="https://v.io/glossary.html#blessing-root">Blessing Root</a> is subject to change
  without notice.
  </span>
  <input type="text" class="hidden" name="macaroon" value="{{.Macaroon}}">

  <h3>Blessing Name</h3>
  <div class="grid">
    <div class="cell">
      <span>{{.BlessingName}}/{{.Extension}}/</span><input name="blessingExtension" type="text" placeholder="extension">
    </div>
    <input type="text" class="hidden" id="timezoneOffset" name="timezoneOffset">
  </div>
  <div>
      The blessing name contains your email and will be visible to any peers that
      this blessing is shared with, e.g. when you make a RPC.
  </div>

  <h4>Caveats</h4>
  <div class="grid caveatRow">
    <div class="cell">
      <span class="selected RevocationCaveatSelected">Active until revoked</span>
      <span class="selected ExpiryCaveatSelected hidden">Expires on</span>
      <span class="selected MethodCaveatSelected hidden">Allowed methods are</span>
      <span class="selected PeerBlessingsCaveatSelected hidden">Allowed peers are</span>

      <select name="caveat" class="caveats hidden">
        <option name="RevocationCaveat" value="RevocationCaveat" class="cavOption">Active until revoked</option>
        <option name="ExpiryCaveat" value="ExpiryCaveat" class="cavOption">Expires on</option>
        <option name="MethodCaveat" value="MethodCaveat" class="cavOption">Allowed methods are</option>
        <option name="PeerBlessingsCaveat" value="PeerBlessingsCaveat" class="cavOption">Allowed peers are</option>
      </select>
    </div>
    <div class="cell">
      <input type="text" class="caveatInput hidden" id="RevocationCaveat" name="RevocationCaveat">
      <input type="datetime-local" class="caveatInput expiry hidden" id="ExpiryCaveat" name="ExpiryCaveat">
      <input type="text" id="MethodCaveat" class="caveatInput hidden" name="MethodCaveat" placeholder="comma-separated method list">
      <input type="text" id="PeerBlessingsCaveat" class="caveatInput hidden" name="PeerBlessingsCaveat" placeholder="comma-separated blessing list">
    </div>

    <div class="cell">
      <a href="#" class="right addMore">Add more</a>
    </div>
    </div>
  </div>
  </br>
  <div class="grid">
    <button class="cell button-passive" id="cancel">Cancel</button>
    <button class="cell button-primary" type="submit">Bless</button>
    <div class="cell"></div>
    <div class="cell"></div>
  </div>
  <div>
  By clicking "Bless", you consent to be bound by
  Google's general <a href="https://www.google.com/intl/en/policies/terms/">Terms of Service</a>,
  the <a href="https://developers.google.com/terms/">Google APIs Terms of Service</a>,
  and Google's general <a href="https://www.google.com/intl/en/policies/privacy/">Privacy Policy</a>.
  </div>
  </form>
</main>

</body>
</html>`))

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package templates

import "html/template"

var Home = template.Must(home.Parse(headPartial))

var home = template.Must(template.New("main").Parse(`<!doctype html>
<html>
<head>
  {{template "head" .}}
  <title>Vanadium Identity Provider</title>
</head>

<body class="home-layout">
<main>
<section class="intro">
  <div class="intro-container">
    <h1 class="head">
      Vanadium Identity Provider
    </h1>

    <h3>
      This is a Vanadium Identity Provider that provides blessings with the
      name prefix {{.Self}}.
    </h3>

    <div class="buttons grid">
      <a href="/auth/google/{{.ListBlessingsRoute}}" class="button-passive cell">
        Your Blessings
      </a>
    </div>
  </div>
</section>

<section class="mission">
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
  </div>
</section>

<section class="mission">
  <div class="grid">
    {{if .ListBlessingsRoute}}
    <div class="cell">
      <h2>Blessings Log</h2>
      <p>
        You can <a class="btn btn-xs btn-primary" href="/auth/google/{{.ListBlessingsRoute}}">enumerate</a>
        blessings provided with your email address.
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
</section>

<footer>
  <nav class="main">
    <a href="https://github.com/veyron/release-issues/issues/new?labels=www">Site Feedback</a>
  </nav>

  <nav class="social">
    <a href="https://github.com/vanadium" class="icon-github"></a>
    <a href="https://twitter.com/vdotio" class="icon-twitter"></a>
  </nav>
</footer>
</main>
</body>
</html>`))

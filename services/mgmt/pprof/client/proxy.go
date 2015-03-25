// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package client implement a client-side proxy of a vanadium server's pprof
// interface.
// It is functionally equivalent to http://golang.org/pkg/net/http/pprof/,
// except that the data comes from a remote vanadium server, and the handlers are
// not registered in DefaultServeMux.
package client

import (
	"bufio"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/services/mgmt/pprof"
	"v.io/v23/vtrace"
)

// StartProxy starts the pprof proxy to a remote pprof object.
func StartProxy(ctx *context.T, name string) (net.Listener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &proxy{ctx, name}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Location", "/pprof/")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/pprof/", p.index)
	mux.HandleFunc("/pprof/cmdline", p.cmdLine)
	mux.HandleFunc("/pprof/profile", p.profile)
	mux.HandleFunc("/pprof/symbol", p.symbol)

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  time.Hour,
		WriteTimeout: time.Hour,
	}
	go server.Serve(listener)
	return listener, nil
}

type proxy struct {
	ctx  *context.T
	name string
}

func replyUnavailable(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintf(w, "%v", err)
}

// index serves an html page for /pprof/ and, indirectly, raw data for /pprof/*
func (p *proxy) index(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/pprof/") {
		name := strings.TrimPrefix(r.URL.Path, "/pprof/")
		if name != "" {
			p.sendProfile(name, w, r)
			return
		}
	}
	c := pprof.PProfClient(p.name)
	ctx, _ := vtrace.SetNewTrace(p.ctx)
	profiles, err := c.Profiles(ctx)
	if err != nil {
		replyUnavailable(w, err)
		return
	}
	if err := indexTmpl.Execute(w, profiles); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%v", err)
		return
	}
}

// sendProfile sends the requested profile as a response.
func (p *proxy) sendProfile(name string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	debug, _ := strconv.Atoi(r.FormValue("debug"))
	c := pprof.PProfClient(p.name)
	ctx, _ := vtrace.SetNewTrace(p.ctx)
	prof, err := c.Profile(ctx, name, int32(debug))
	if err != nil {
		replyUnavailable(w, err)
		return
	}
	iterator := prof.RecvStream()
	for iterator.Advance() {
		_, err := w.Write(iterator.Value())
		if err != nil {
			return
		}
	}
	if err := iterator.Err(); err != nil {
		replyUnavailable(w, err)
		return
	}
	if err := prof.Finish(); err != nil {
		replyUnavailable(w, err)
		return
	}
}

// profile replies with a CPU profile.
func (p *proxy) profile(w http.ResponseWriter, r *http.Request) {
	sec, _ := strconv.ParseUint(r.FormValue("seconds"), 10, 64)
	if sec == 0 {
		sec = 30
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	c := pprof.PProfClient(p.name)
	ctx, _ := vtrace.SetNewTrace(p.ctx)
	prof, err := c.CpuProfile(ctx, int32(sec))
	if err != nil {
		replyUnavailable(w, err)
		return
	}
	iterator := prof.RecvStream()
	for iterator.Advance() {
		_, err := w.Write(iterator.Value())
		if err != nil {
			return
		}
	}
	if err := iterator.Err(); err != nil {
		replyUnavailable(w, err)
		return
	}
	if err := prof.Finish(); err != nil {
		replyUnavailable(w, err)
		return
	}
}

// cmdLine replies with the command-line arguments of the process.
func (p *proxy) cmdLine(w http.ResponseWriter, r *http.Request) {
	c := pprof.PProfClient(p.name)
	ctx, _ := vtrace.SetNewTrace(p.ctx)
	cmdline, err := c.CmdLine(ctx)
	if err != nil {
		replyUnavailable(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, strings.Join(cmdline, "\x00"))
}

// symbol replies with a map of program counters to object name.
func (p *proxy) symbol(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	var b *bufio.Reader
	if r.Method == "POST" {
		b = bufio.NewReader(r.Body)
	} else {
		b = bufio.NewReader(strings.NewReader(r.URL.RawQuery))
	}
	pcList := []uint64{}
	for {
		word, err := b.ReadSlice('+')
		if err == nil {
			word = word[0 : len(word)-1] // trim +
		}
		pc, _ := strconv.ParseUint(string(word), 0, 64)
		if pc != 0 {
			pcList = append(pcList, pc)
		}
		if err != nil {
			if err != io.EOF {
				replyUnavailable(w, err)
				return
			}
			break
		}
	}
	c := pprof.PProfClient(p.name)
	ctx, _ := vtrace.SetNewTrace(p.ctx)
	pcMap, err := c.Symbol(ctx, pcList)
	if err != nil {
		replyUnavailable(w, err)
		return
	}
	if len(pcMap) != len(pcList) {
		replyUnavailable(w, fmt.Errorf("received the wrong number of results. Got %d, want %d", len(pcMap), len(pcList)))
		return
	}
	// The pprof tool always wants num_symbols to be non-zero, even if no
	// symbols are returned.. The actual value doesn't matter.
	fmt.Fprintf(w, "num_symbols: 1\n")
	for i, v := range pcMap {
		if len(v) > 0 {
			fmt.Fprintf(w, "%#x %s\n", pcList[i], v)
		}
	}
}

var indexTmpl = template.Must(template.New("index").Parse(`<html>
<head>
<title>/pprof/</title>
</head>
/pprof/<br>
<br>
<body>
profiles:<br>
<table>
{{range .}}<tr><td align=right><td><a href="/pprof/{{.}}?debug=1">{{.}}</a>
{{end}}
</table>
<br>
<a href="/pprof/goroutine?debug=2">full goroutine stack dump</a><br>
</body>
</html>
`))

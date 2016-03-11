// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"fmt"
	"html/template"
	"net/http"
	"path"
	"reflect"
	"strings"
	"time"

	"v.io/x/ref/services/ben"
)

// NewHTTPHandler returns a handler that provides web interface for browsing
// benchmark results in store.
func NewHTTPHandler(store Store) http.Handler {
	return &handler{store}
}

type handler struct {
	store Store
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch path.Base(r.URL.Path) {
	case "sortable.js":
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(jsSortable)
		return
	case "sortable.css":
		w.Header().Set("Content-Type", "text/css")
		w.Write(cssSortable)
		return
	}
	if id := strings.TrimSpace(r.FormValue("id")); len(id) > 0 {
		bm, itr := h.store.Runs(id)
		defer itr.Close()
		h.runs(w, bm, itr)
		return
	}
	if qstr := strings.TrimSpace(r.FormValue("q")); len(qstr) > 0 {
		query, err := ParseQuery(qstr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			args := struct {
				Query string
				Error error
			}{qstr, err}
			executeTemplate(w, tmplBadQuery, args)
			return
		}
		h.handleQuery(w, query)
		return
	}
	if src := strings.TrimSpace(r.FormValue("s")); len(src) > 0 {
		h.describeSource(w, src)
		return
	}
	executeTemplate(w, tmplHome, nil)
}

func (h *handler) handleQuery(w http.ResponseWriter, query *Query) {
	bmarks := h.store.Benchmarks(query)
	defer bmarks.Close()
	if !bmarks.Advance() {
		executeTemplate(w, tmplNoBenchmarks, query)
		return
	}
	bm := bmarks.Value()
	// Advance once more, if there are more benchmarks (different names,
	// different scenarios, different uploaders) for the same query, then
	// present a list to choose from.
	if !bmarks.Advance() {
		itr := bmarks.Runs()
		defer itr.Close()
		h.runs(w, bm, itr)
		return
	}
	h.benchmarks(w, query, bm, bmarks)
}

func (h *handler) benchmarks(w http.ResponseWriter, query *Query, first Benchmark, itr BenchmarkIterator) {
	var (
		cancel = make(chan struct{})
		items  = make(chan Benchmark, 2)
		errs   = make(chan error, 1)
	)
	defer close(cancel)
	items <- first
	items <- itr.Value()
	go func() {
		defer close(errs)
		defer close(items)
		for itr.Advance() {
			select {
			case items <- itr.Value():
			case <-cancel:
				return
			}
			if err := itr.Err(); err != nil {
				errs <- err
			}
		}
	}()
	args := struct {
		Query *Query
		Items <-chan Benchmark
		Err   <-chan error
	}{
		Query: query,
		Items: items,
		Err:   errs,
	}
	executeTemplate(w, tmplBenchmarks, args)
}

func (h *handler) runs(w http.ResponseWriter, bm Benchmark, itr RunIterator) {
	type item struct {
		Run          ben.Run
		SourceCodeID string
		UploadTime   time.Time
		Index        int
	}
	var (
		cancel = make(chan struct{})
		items  = make(chan item)
		errs   = make(chan error, 1)
	)
	defer close(cancel)
	go func() {
		defer close(errs)
		defer close(items)
		idx := 0
		for itr.Advance() {
			idx++
			r, s, t := itr.Value()
			select {
			case items <- item{r, s, t, idx}:
			case <-cancel:
				return
			}
		}
		if err := itr.Err(); err != nil {
			errs <- err
		}
	}()
	args := struct {
		Benchmark Benchmark
		Items     <-chan item
		Err       <-chan error
	}{
		Benchmark: bm,
		Items:     items,
		Err:       errs,
	}
	executeTemplate(w, tmplRuns, args)
	return
}

func (h *handler) describeSource(w http.ResponseWriter, src string) {
	w.Header().Set("Content-Type", "text/plain")
	code, err := h.store.DescribeSource(src)
	if err != nil {
		fmt.Fprintf(w, "ERROR:%v", err)
		return
	}
	fmt.Fprintln(w, code)
}

func executeTemplate(w http.ResponseWriter, tmpl *template.Template, args interface{}) {
	if err := tmpl.Execute(w, args); err != nil {
		fmt.Fprintf(w, "ERROR:%v", err)
	}

}

func templateParse(t *template.Template, contents ...string) *template.Template {
	for _, c := range contents {
		t = template.Must(t.Parse(c))
	}
	return t
}

var (
	htmlStyling = `{{define "styling"}}
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<link rel="stylesheet" href="https://storage.googleapis.com/code.getmdl.io/1.0.6/material.indigo-pink.min.css">
<script src="https://storage.googleapis.com/code.getmdl.io/1.0.6/material.min.js"></script>
<link rel="stylesheet" href="https://fonts.googleapis.com/icon?family=Material+Icons">
{{end}}`
	htmlFooter = `{{define "footer"}}
<footer class="mdl-mini-footer">
  <div class="mdl-mini-footer__left-section">
    <ul class="mdl-mini-footer__link-list">
    <li><a href="/">Home</a></li>
    <li><a href="https://github.com/vanadium/go.ref/tree/master/services/ben">Github</a></li>
    </ul>
  </div>
</footer>
{{end}}`
	jsSortable = []byte(`
var tables = document.getElementsByClassName('mdl-data-table-sortable');
[].forEach.call(tables, makeTableSortable)

function makeTableSortable(tb) {
    var data = extractData();
    var headers = tb.getElementsByClassName('mdl-data-table__header-sortable');
    [].forEach.call(headers, makeColumnSortable)

    function makeColumnSortable(th, index) {
        appendSortIcon(th);
        th.isSortedAsc = th.classList.contains('mdl-data-table__header--sorted-ascending');
        var isnum = th.classList.contains('mdl-data-table__header-sortable-numeric');
        th.addEventListener('click', function() {
            var sortAsc = !th.isSortedAsc;
            [].forEach.call(headers, function reset(h) {
                h.classList.remove('mdl-data-table__header--sorted-ascending');
                h.classList.remove('mdl-data-table__header--sorted-descending');
                h.isSortedAsc = null;
            });
            th.classList.add(sortAsc ? 'mdl-data-table__header--sorted-ascending': 'mdl-data-table__header--sorted-descending');
            sortData(index, isnum, sortAsc);
            reorderRows();
            th.isSortedAsc = sortAsc;
        });
    }

    function sortData(index, isnum, ascending) {
        data.sort(function(a, b) {
            if(isnum) {
                var anum = Number(a.data[index]);
                var bnum = Number(b.data[index]);
                if(ascending) {
                        return anum-bnum;
                } else {
                        return bnum-anum;
                }
            }
            if(ascending) {
                return a.data[index].localeCompare(b.data[index]);
            } else {
                return b.data[index].localeCompare(a.data[index]);
            }
        });
    }

    function reorderRows() {
        for(var i = 0; i < data.length; i++ ) {
            tb.appendChild(data[i].row);
        }
        return data;
    }

    function extractData() {
        var data = [];
        for(var i = 1; i < tb.rows.length; i++ ) {
            var row = tb.rows[i];
            data[i-1] = {row: row, data: []};
            for(var j = 0; j < row.cells.length; j++ ) {
              var cell = row.cells[j];
              var target = cell.getElementsByClassName('mdl-data-table__cell-data')[0] || cell;
              data[i-1].data[j] = target.textContent;
            }
        }
        return data;
    }

    function appendSortIcon(th) {
        var icon = document.createElement('span');
        icon.className = 'mdl-data-table__sorticon';
        th.appendChild(icon);
    }
}`)
	cssSortable = []byte(`
.mdl-data-table__sorticon {
    transition: 0.2s ease-in-out;
    display: inline-block;
    width: 18px;
    height: 18px;
    margin-left: 5px;
    margin-right: 5px;
    vertical-align: sub;
    opacity: 0;
    background: url('data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" viewBox="0 0 24 24" fit="" preserveAspectRatio="xMidYMid meet" style="pointer-events: none; display: block;"><path d="M4 12l1.41 1.41L11 7.83V20h2V7.83l5.58 5.59L20 12l-8-8-8 8z"></path></svg>') no-repeat;
}

.mdl-data-table__header--sorted-ascending,
.mdl-data-table__header--sorted-descending {
    color: rgba(0, 0, 0, .87) !important;
}

.mdl-data-table__header--sorted-ascending .mdl-data-table__sorticon {
    opacity: 1 !important;
}

.mdl-data-table__header--sorted-descending .mdl-data-table__sorticon {
    opacity: 1 !important;
    transform: rotate(180deg);
}

.mdl-data-table__header-sortable {
    cursor: default;
}

.mdl-data-table__header-sortable:hover {
    color: rgba(0, 0, 0, .87) !important;
}

.mdl-data-table__header-sortable:hover .mdl-data-table__sorticon {
    opacity: 0.26;
}
</style>
`)
	tmplHome = templateParse(template.New(".home"), `<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
    <title>Benchmark Results Archive</title>
    {{template "styling"}}
    <style>
    .fixed-width { font-family: monospace; }
    </style>
</head>
<body>
  <div class="mdl-layout mdl-js-layout">
    <main class="mdl-layout__content">
    <div class="mdl-grid">
    <div class="mdl-cell mdl-cell--1-col"></div>
    <div class="mdl-cell mdl-cell--6-col">
    <form action="#">
        <div class="mdl-textfield mdl-js-textfield">
          <input class="mdl-textfield__input" type="text" id="q" name="q"/>
          <label class="mdl-textfield__label" for="q">Search query</label>
        </div>
        <input value="Search" type="submit" class="mdl-button mdl-js-button mdl-button--raised mdl-button--colored"/>
    </form>
    Sample queries:
    <ul>
        <li><a href="/?q=VomEncode">VomEncode</a> - All benchmarks with names matching VomEncode</li>
        <li><a href="/?q=os:linux+cpu:amd64+v.io/v23/security">os:linux cpu:amd64 v.io/v23/security</a> - Benchmarks on desktop linux for the <span class="fixed-width">v.io/v23/security</span> package</li>
    </ul>
    Search operators:
    <ul>
       <li>os - Operating system, e.g., os:linux, os:&quot;Ubuntu 14.04&quot; etc.</li>
       <li>cpu - CPU, e.g., cpu:arm, cpu:xeon etc.</li>
       <li>uploader - Identity of uploader, e.g., uploader:janedoe</li>
       <li>label - Label assigned by the uploader, e.g., label:mylabel</li>
    </ul>
    </div>
    <div class="mdl-cell mdl-cell--1-col"></div>
    </div>
    </main>
    {{template "footer"}}
  </div>
</body>
</html>
`, htmlStyling, htmlFooter)
	tmplBadQuery = templateParse(template.New(".badquery"), `
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
    <title>ERROR: Benchmark Results Archive</title>
    {{template "styling"}}
</head>
<body>
  <div class="mdl-layout mdl-js-layout">
    <main class="mdl-layout__content">
      <div class="mdl-grid">
      <div class="mdl-cell mdl-cell--1-col"></div>
      <div class="mdl-cell mdl-cell--6-col">
      <i class="material-icons">error</i>[{{.Query}}] is not valid: {{.Error}}
      </div>
      <div class="mdl-cell mdl-cell--1-col"></div>
      </div>
    </main>
    {{template "footer"}}
  </div>
</body>
</html>
`, htmlStyling, htmlFooter)
	tmplNoBenchmarks = templateParse(template.New(".nobenchmarks"), `
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
    <title>Benchmark Results Archive</title>
    {{template "styling"}}
</head>
<body>
  <div class="mdl-layout mdl-js-layout">
    <main class="mdl-layout__content">
    <div class="mdl-grid">
    <div class="mdl-cell mdl-cell--1-col"></div>
    <div class="mdl-cell mdl-cell--6-col">
    <i class="material-icons">info</i>No results for [{{.}}]
    </div>
    <div class="mdl-cell mdl-cell--1-col"></div>
    </div>
    </main>
    {{template "footer"}}
  </div>
</body>
</html>
`, htmlStyling, htmlFooter)
	tmplBenchmarks = templateParse(template.New(".benchmarks").Funcs(
		template.FuncMap{
			"refineQuery": func(q *Query, field, value string) (*Query, error) {
				ret := *q
				f := reflect.ValueOf(&ret).Elem().FieldByName(field)
				var invalid reflect.Value
				if f == invalid {
					return nil, fmt.Errorf("%q is not a valid query field", field)
				}
				f.Set(reflect.ValueOf(value))
				return &ret, nil
			},
		}), `
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
    <title>Benchmark Results Archive</title>
    {{template "styling"}}
    <link rel="stylesheet" href="sortable.css">
</head>
<body class="mdl-demo mdl-color--grey-100 mdl-color-text--grey-700 mdl-base">
  <div class="mdl-layout mdl-js-layout mdl-layout--fixed-header">
    <main class="mdl-layout__content">
      <section class="section--center mdl-grid mdl-grid--no-spacing mdl-shadow--2dp">
        <div class="mdl-card__supporting-text">
        <form action="#">
            <div class="mdl-textfield mdl-js-textfield">
              <input class="mdl-textfield__input" type="text" id="q" name="q" value="{{.Query}}"/>
              <label class="mdl-textfield__label" for="q">{{.Query}}</label>
            </div>
            <input value="Search" type="submit" class="mdl-button mdl-js-button mdl-button--raised mdl-button--colored"/>
	    {{if .Query.CPU}}
	    <a href="/?q={{refineQuery .Query "CPU" "" | urlquery}}" class="mdl-button mdl-js-button"><i class="material-icons">remove</i>CPU</a>
	    {{end}}
	    {{if .Query.OS}}
	    <a href="/?q={{refineQuery .Query "OS" "" | urlquery}}" class="mdl-button mdl-js-button"><i class="material-icons">remove</i>OS</a>
	    {{end}}
	    {{if .Query.Uploader}}
	    <a href="/?q={{refineQuery .Query "Uploader" "" | urlquery}}" class="mdl-button mdl-js-button"><i class="material-icons">remove</i>Uploader</a>
	    {{end}}
	    {{if .Query.Label}}
	    <a href="/?q={{refineQuery .Query "Label" "" | urlquery}}" class="mdl-button mdl-js-button"><i class="material-icons">remove</i>Label</a>
	    {{end}}
        </form>

        </div>
      </section>
      <section class="section--center mdl-grid mdl-grid--no-spacing mdl-shadow--2dp">
      <div class="mdl-card mdl-cell mdl-cell--12-col">
        <div class="mdl-card__supporting-text">
          <h4>Benchmarks</h4>
          <table class="mdl-data-table mdl-js-data-table mdl-shadow--2dp mdl-data-table-sortable">
            <thead>
            <tr>
              <th class="mdl-data-table__header-sortable">Name</th>
              <th class="mdl-data-table__header-sortable mdl-data-table__header-sortable-numeric" title="time per iteration">time/op</th>
              <th class="mdl-data-table__header-sortable" title="operating system on which benchmarks were run">OS</th>
              <th class="mdl-data-table__header-sortable" title="CPU architecture on which benchmarks were run">CPU</th>
              <th class="mdl-data-table__header-sortable" title="who uploaded results for this benchmark">Uploader</th>
              <th class="mdl-data-table__header-sortable">Label</th>
              <th class="mdl-data-table__header-sortable">Last Update</th>
            </tr>
            </thead>
            <tbody>
                {{range .Items}}
                <tr>
                <td class="mdl-data-table__cell--non-numeric"><a href="?id={{.ID | urlquery}}">{{.Name}}</a></td>
                <td><div id="time_{{.ID}}">{{.PrettyTime}}</div><div class="mdl-tooltip" for="time_{{.ID}}"><span class="mdl-data-table__cell-data">{{.NanoSecsPerOp}}</span>ns</div></td>
                <td class="mdl-data-table__cell--non-numeric"><div id="os_{{.ID}}"><a href="/?q={{refineQuery $.Query "OS" .Scenario.Os.Version | urlquery}}">{{.Scenario.Os.Name}}</a></div><div class="mdl-tooltip mdl-data-table__cell-data" for="os_{{.ID}}">{{.Scenario.Os.Version}}</div></td>
                <td class="mdl-data-table__cell--non-numeric"><div id="cpu_{{.ID}}"><a href="/?q={{refineQuery $.Query "CPU" .Scenario.Cpu.Description | urlquery}}">{{.Scenario.Cpu.Architecture}}</a></div><div class="mdl-tooltip mdl-data-table__cell-data" for="cpu_{{.ID}}">{{.Scenario.Cpu.Description}}</div></td>
                <td class="mdl-data-table__cell--non-numeric"><a href="/?q={{refineQuery $.Query "Uploader" .Uploader | urlquery}}">{{.Uploader}}</a></td>
                <td class="mdl-data-table__cell--non-numeric"><a href="/?q={{refineQuery $.Query "Label" .Scenario.Label | urlquery}}">{{.Scenario.Label}}</a></td>
                <td class="mdl-data-table__cell--non-numeric">{{.LastUpdate}}</td>
                </tr>
                {{end}}
                {{range .Err}}
                <tr><td colspan=5><i class="material-icons">error</i>{{.}}</td></tr>
                {{end}}
            </tbody>
          </table>
        </div>
      </div>
      </section>
    </main>
    <script src="/sortable.js"></script>
    {{template "footer"}}
  </div>
</body>
</html>
`, htmlStyling, htmlFooter)
	tmplRuns = templateParse(template.New(".runs"), `
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
    <title>Benchmark Results Archive</title>
    {{template "styling"}}
    <link rel="stylesheet" href="sortable.css">
    <style>
    .fixed-width { font-family: monospace; }
    </style>
</head>
<body class="mdl-demo mdl-color--grey-100 mdl-color-text--grey-700 mdl-base">
  <div class="mdl-layout mdl-js-layout mdl-layout--fixed-header">
    <main class="mdl-layout__content">
      {{with .Benchmark}}
      <section class="section--center mdl-grid mdl-grid--no-spacing mdl-shadow--2dp">
      <div class="mdl-card mdl-cell mdl-cell--12-col">
        <div class="mdl-card__supporting-text">
          <h4><a href="?q={{.Name | urlquery}}">{{.Name}}</a></h4>
          <table class="mdl-data-table mdl-js-data-table mdl-shadow--2dp">
            <tbody>
              <tr>
                <td class="mdl-data-table__cell--non-numeric">OS</td>
                <td class="mdl-data-table__cell--non-numeric">{{.Scenario.Os.Name}} ({{.Scenario.Os.Version}})</td>
              </tr>
              <tr>
                <td class="mdl-data-table__cell--non-numeric">CPU</td>
                <td class="mdl-data-table__cell--non-numeric">{{.Scenario.Cpu.Architecture}} ({{.Scenario.Cpu.Description}})</td>
              </tr>
	      {{with .Scenario.Cpu.ClockSpeedMhz}}
	      <tr>
                <td class="mdl-data-table__cell--non-numeric">ClockSpeed</td>
                <td class="mdl-data-table__cell--non-numeric">{{.}} MHz</td>
	      </tr>
	      {{end}}
              <tr>
                <td class="mdl-data-table__cell--non-numeric">Uploader</td>
                <td class="mdl-data-table__cell--non-numeric">{{.Uploader}}</td>
              </tr>
              {{with .Scenario.Label}}
              <tr>
                <td class="mdl-data-table__cell--non-numeric">Label</td>
                <td class="mdl-data-table__cell--non-numeric">{{.}}</td>
              </tr>
              {{end}}
            </tbody>
          </table>
        </div>
      </div>
      </section>
      {{end}}

      <section class="section--center mdl-grid mdl-grid--no-spacing mdl-shadow--2dp">
      <div class="mdl-card mdl-cell mdl-cell--12-col">
        <div class="mdl-card__supporting-text">
          <h4>Runs</h4>
          <table class="mdl-data-table mdl-js-data-table mdl-shadow--2dp fixed-width mdl-data-table-sortable">
            <thead>
            <tr>
              <th class="mdl-data-table__header-sortable mdl-data-table__header-sortable-numeric" title="time per iteration">time/op</th>
              <th class="mdl-data-table__header-sortable mdl-data-table__header-sortable-numeric" title="number of memory allocations per iteration">allocs/op</th>
              <th class="mdl-data-table__header-sortable mdl-data-table__header-sortable-numeric" title="number of bytes of memory allocations per iteration">allocated bytes/op</th>
              <th class="mdl-data-table__header-sortable mdl-data-table__header-sortable-numeric" title="megabytes processed per second">MB/s</th>
              <th class="mdl-data-table__header-sortable" title="timestamp when results were uploaded" class="mdl-data-table__cell--non-numeric mdl-data-table__header--sorted-descending">Uploaded</th>
              <th title="description of source code" class="mdl-data-table__cell--non-numeric">SourceCode</th>
              <th class="mdl-data-table__header-sortable mdl-data-table__header-sortable-numeric">Iterations</th>
              <th class="mdl-data-table__header-sortable mdl-data-table__header-sortable-numeric" title="parallelism used to run benchmark, e.g., GOMAXPROCS for Go benchmarks">Parallelism</th>
            </tr>
            </thead>
            <tbody>
              {{range .Items}}
              <tr>
                <td><div id="run_{{.Index}}">{{.Run.PrettyTime}}</div><div class="mdl-tooltip mdl-data-table__cell-data" for="run_{{.Index}}">{{.Run.NanoSecsPerOp}}ns</div></td>
                <td>{{if .Run.AllocsPerOp}}{{.Run.AllocsPerOp}}{{end}}</td>
                <td>{{if .Run.AllocedBytesPerOp}}{{.Run.AllocedBytesPerOp}}{{end}}</td>
                <td>{{if .Run.MegaBytesPerSec}}{{.Run.MegaBytesPerSec}}{{end}}</td>
                <td class="mdl-data-table__cell--non-numeric">{{.UploadTime}}</td>
                <td class="mdl-data-table__cell--non-numeric"><a href="?s={{urlquery .SourceCodeID}}">(sources)</a></td>
                <td>{{.Run.Iterations}}</td>
                <td>{{.Run.Parallelism}}</td>
              </tr>
              {{end}}
              {{range .Err}}
              <tr><td colspan=8><i class="material-icons">error</i>{{.}}</td></tr>
              {{end}}
            </tbody>
          </table>
        </div>
      </div>
      </section>
    </main>
    <script src="/sortable.js"></script>
    {{template "footer"}}
  </div>
</body>
</html>
`, htmlStyling, htmlFooter)
)

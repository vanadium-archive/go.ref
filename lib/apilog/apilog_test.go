// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apilog_test

import (
	"bufio"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"v.io/x/lib/vlog"

	"v.io/x/ref/lib/apilog"
)

func readLogFiles(dir string) ([]string, error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var contents []string
	for _, fi := range files {
		// Skip symlinks to avoid double-counting log lines.
		if !fi.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(filepath.Join(dir, fi.Name()))
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if line := scanner.Text(); len(line) > 0 && line[0] == 'I' {
				contents = append(contents, line)
			}
		}
	}
	return contents, nil
}

func myLoggedFunc() {
	f := apilog.LogCall(nil, "entry")
	f(nil, "exit")
}

func TestLogCall(t *testing.T) {
	dir, err := ioutil.TempDir("", "logtest")
	defer os.RemoveAll(dir)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	logger := vlog.NewLogger("testHeader")
	logger.Configure(vlog.LogDir(dir), vlog.Level(2))
	saveLog := apilog.Log()
	defer func() { apilog.SetLog(saveLog) }()
	apilog.SetLog(logger)
	myLoggedFunc()
	logger.FlushLog()
	contents, err := readLogFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if want, got := 2, len(contents); want != got {
		t.Errorf("Expected %d info lines, got %d instead", want, got)
	}
	logCallLineRE := regexp.MustCompile(`\S+ \S+\s+\S+ ([^:]*):.*(call|return)\[(\S*)`)
	for _, line := range contents {
		match := logCallLineRE.FindStringSubmatch(line)
		if len(match) != 4 {
			t.Errorf("failed to match %s", line)
			continue
		}
		fileName, callType, funcName := match[1], match[2], match[3]
		if fileName != "apilog_test.go" {
			t.Errorf("unexpected file name: %s", fileName)
			continue
		}
		if callType != "call" && callType != "return" {
			t.Errorf("unexpected call type: %s", callType)
		}
		if funcName != "apilog_test.myLoggedFunc" {
			t.Errorf("unexpected func name: %s", funcName)
		}
	}
}

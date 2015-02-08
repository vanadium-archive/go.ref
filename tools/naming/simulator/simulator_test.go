package main_test

//go:generate v23 integration generate .

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"testing"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil/v23tests"
)

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func V23TestSimulator(t v23tests.T) {
	binary := t.BuildGoPkg("v.io/core/veyron/tools/naming/simulator")
	files, err := ioutil.ReadDir("./testdata")
	if err != nil {
		t.Fatal(err)
	}
	scripts := []string{}
	re := regexp.MustCompile(`.*\.scr`)
	for _, f := range files {
		if !f.IsDir() && re.MatchString(f.Name()) {
			scripts = append(scripts, "./testdata/"+f.Name())
		}
	}
	for _, script := range scripts {
		if testing.Verbose() {
			fmt.Fprintf(os.Stderr, "Script %v\n", script)
		}
		invocation := binary.Start("--file", script)
		var stdout, stderr bytes.Buffer
		if err := invocation.Wait(&stdout, &stderr); err != nil {
			fmt.Fprintf(os.Stderr, "Script %v failed\n", script)
			fmt.Fprintln(os.Stderr, stdout.String())
			fmt.Fprintln(os.Stderr, stderr.String())
			t.Error(err)
		}
	}
}

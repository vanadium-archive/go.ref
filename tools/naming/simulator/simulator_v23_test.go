package main_test

//go:generate v23 test generate .

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"testing"

	"v.io/x/ref/lib/testutil/v23tests"
)

//go:generate v23 test generate

// HACK: This is a hack to force v23 test generate to generate modules.Dispatch in TestMain.
// TODO(suharshs,cnicolaou): Find a way to get rid of this dummy subprocesses.
func dummy(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	return nil
}

func V23TestSimulator(t *v23tests.T) {
	binary := t.BuildGoPkg("v.io/x/ref/tools/naming/simulator")
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
		scriptFile, err := os.Open(script)
		if err != nil {
			t.Fatalf("Open(%q) failed: %v", script, err)
		}
		invocation := binary.WithStdin(scriptFile).Start()
		var stdout, stderr bytes.Buffer
		if err := invocation.Wait(&stdout, &stderr); err != nil {
			fmt.Fprintf(os.Stderr, "Script %v failed\n", script)
			fmt.Fprintln(os.Stderr, stdout.String())
			fmt.Fprintln(os.Stderr, stderr.String())
			t.Error(err)
		}
	}
}

package testdata

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"testing"

	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil/integration"

	_ "v.io/core/veyron/profiles/static"
)

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestSimulator(t *testing.T) {
	env := integration.NewTestEnvironment(t)
	defer env.Cleanup()
	binary := env.BuildGoPkg("v.io/core/veyron/tools/naming/simulator")
	files, err := ioutil.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	scripts := []string{}
	re := regexp.MustCompile(`.*\.scr`)
	for _, f := range files {
		if !f.IsDir() && re.MatchString(f.Name()) {
			scripts = append(scripts, f.Name())
		}
	}
	for _, script := range scripts {
		invocation := binary.Start("--file", script)
		output := invocation.Output()
		var buf bytes.Buffer
		if err := invocation.Wait(nil, bufio.NewWriter(&buf)); err != nil {
			errorOutput := string(buf.Bytes())
			fmt.Fprintf(os.Stderr, "Script %v failed\n", script)
			fmt.Fprintln(os.Stderr, output)
			fmt.Fprintln(os.Stderr, errorOutput)
			t.Fatal(err)
		}
	}
}

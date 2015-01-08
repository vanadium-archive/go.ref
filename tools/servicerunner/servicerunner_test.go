// Runs the servicerunner binary and checks that it outputs a JSON line to
// stdout with the expected variables.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"testing"
)

func check(t *testing.T, err error) {
	if err != nil {
		t.Fatal(err)
	}
}

func TestMain(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "servicerunner_test")
	check(t, err)
	defer os.RemoveAll(tmpdir)
	os.Setenv("TMPDIR", tmpdir)

	bin := path.Join(tmpdir, "servicerunner")
	fmt.Println("Building", bin)
	check(t, exec.Command("v23", "go", "build", "-o", bin, "v.io/core/veyron/tools/servicerunner").Run())

	cmd := exec.Command(bin)
	stdout, err := cmd.StdoutPipe()
	check(t, err)
	check(t, cmd.Start())

	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	check(t, err)
	vars := map[string]string{}
	check(t, json.Unmarshal(line, &vars))
	fmt.Println(vars)
	expectedVars := []string{
		"VEYRON_CREDENTIALS",
		"MT_NAME",
		"PROXY_ADDR",
		"WSPR_ADDR",
		"TEST_IDENTITYD_ADDR",
		"TEST_IDENTITYD_HTTP_ADDR",
	}
	for _, name := range expectedVars {
		if _, ok := vars[name]; !ok {
			t.Error("Missing", name)
		}
	}

	check(t, cmd.Process.Kill())
}

package impl_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/security"
	"v.io/core/veyron2/services/mgmt/application"
	"v.io/core/veyron2/services/mgmt/device"

	"v.io/core/veyron/tools/mgmt/device/impl"
)

func TestInstallLocalCommand(t *testing.T) {
	shutdown := initTest()
	defer shutdown()

	tape := NewTape()
	server, endpoint, err := startServer(t, gctx, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)
	// Setup the command-line.
	cmd := impl.Root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	deviceName := naming.JoinAddressName(endpoint.String(), "")
	appTitle := "Appo di tutti Appi"
	for i, c := range []struct {
		args         []string
		stderrSubstr string
	}{
		{
			[]string{deviceName}, "incorrect number of arguments",
		},
		{
			[]string{deviceName, appTitle}, "missing binary",
		},
		{
			[]string{deviceName, appTitle, "a=b"}, "missing binary",
		},
		{
			[]string{deviceName, appTitle, "foo"}, "binary foo not found",
		},
	} {
		c.args = append([]string{"install-local"}, c.args...)
		if err := cmd.Execute(c.args); err == nil {
			t.Fatalf("test case %d: wrongly failed to receive a non-nil error.", i)
		} else {
			fmt.Fprintln(&stderr, "ERROR:", err)
			if want, got := c.stderrSubstr, stderr.String(); !strings.Contains(got, want) {
				t.Errorf("test case %d: %q not found in stderr: %q", i, want, got)
			}
		}
		if got, expected := len(tape.Play()), 0; got != expected {
			t.Errorf("test case %d: invalid call sequence. Got %v, want %v", got, expected)
		}
		tape.Rewind()
		stdout.Reset()
		stderr.Reset()
	}
	appId := "myBestAppID"
	binary := os.Args[0]
	fi, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("Failed to stat %v: %v", binary, err)
	}
	emptySig := security.Signature{Purpose: []uint8{}, Hash: "", R: []uint8{}, S: []uint8{}}
	emptyBlessings := security.WireBlessings{}
	binarySize := fi.Size()
	cfg := device.Config{"someflag": "somevalue"}
	for i, c := range []struct {
		args         []string
		config       device.Config
		expectedTape interface{}
	}{
		{
			[]string{deviceName, appTitle, binary},
			nil,
			InstallStimulus{"Install", appNameAfterFetch, nil, application.Envelope{Title: appTitle, Binary: binaryNameAfterFetch, Signature: emptySig, Publisher: emptyBlessings}, binarySize},
		},
		{
			[]string{deviceName, appTitle, binary},
			cfg,
			InstallStimulus{"Install", appNameAfterFetch, cfg, application.Envelope{Title: appTitle, Binary: binaryNameAfterFetch, Signature: emptySig, Publisher: emptyBlessings}, binarySize},
		},
		{
			[]string{deviceName, appTitle, "ENV1=V1", "ENV2=V2", binary, "FLAG1=V1", "FLAG2=V2"},
			nil,
			InstallStimulus{"Install", appNameAfterFetch, nil, application.Envelope{Title: appTitle, Binary: binaryNameAfterFetch, Signature: emptySig, Publisher: emptyBlessings, Env: []string{"ENV1=V1", "ENV2=V2"}, Args: []string{"FLAG1=V1", "FLAG2=V2"}}, binarySize},
		},
	} {
		tape.SetResponses([]interface{}{InstallResponse{appId, nil}})
		if c.config != nil {
			jsonConfig, err := json.Marshal(c.config)
			if err != nil {
				t.Fatalf("test case %d: Marshal(%v) failed: %v", i, c.config, err)
			}
			c.args = append([]string{fmt.Sprintf("--config=%s", string(jsonConfig))}, c.args...)
		}
		c.args = append([]string{"install-local"}, c.args...)
		if err := cmd.Execute(c.args); err != nil {
			t.Fatalf("test case %d: %v", i, err)
		}
		if expected, got := fmt.Sprintf("Successfully installed: %q", naming.Join(deviceName, appId)), strings.TrimSpace(stdout.String()); got != expected {
			t.Fatalf("test case %d: Unexpected output from Install. Got %q, expected %q", i, got, expected)
		}
		if got, expected := tape.Play(), []interface{}{c.expectedTape}; !reflect.DeepEqual(expected, got) {
			t.Errorf("test case %d: Invalid call sequence. Got %#v, want %#v", i, got, expected)
		}
		tape.Rewind()
		stdout.Reset()
		stderr.Reset()
	}
}

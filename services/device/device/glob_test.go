// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/device"

	"v.io/x/lib/cmdline"
	"v.io/x/ref/test"

	cmd_device "v.io/x/ref/services/device/device"
)

func simplePrintHandler(entry cmd_device.GlobResult, ctx *context.T, stdout, stderr io.Writer) error {
	fmt.Fprintf(stdout, "%v\n", entry)
	return nil
}

func TestGlob(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()
	tapes := newTapeMap()
	rootTape := tapes.forSuffix("")
	server, endpoint, err := startServer(t, ctx, tapes)
	if err != nil {
		return
	}
	appName := naming.JoinAddressName(endpoint.String(), "app")
	defer stopServer(t, server)

	allGlobArgs := []string{"glob1", "glob2"}
	allGlobResponses := [][]string{
		[]string{"app/3", "app/4", "app/6", "app/5", "app/9", "app/7"},
		[]string{"app/2", "app/1", "app/8"},
	}
	allStatusResponses := map[string][]interface{}{
		"app/1": []interface{}{instanceRunning},
		"app/2": []interface{}{installationUninstalled},
		"app/3": []interface{}{instanceUpdating},
		"app/4": []interface{}{installationActive},
		"app/5": []interface{}{instanceNotRunning},
		"app/6": []interface{}{deviceService},
		"app/7": []interface{}{installationActive},
		"app/8": []interface{}{deviceUpdating},
		"app/9": []interface{}{instanceUpdating},
	}
	outLine := func(suffix string, s device.Status) string {
		r, err := cmd_device.NewGlobResult(appName+"/"+suffix, s)
		if err != nil {
			t.Errorf("NewGlobResult failed: %v", err)
			return ""
		}
		return fmt.Sprintf("%v", *r)
	}
	var (
		app1Out = outLine("1", instanceRunning)
		app2Out = outLine("2", installationUninstalled)
		app3Out = outLine("3", instanceUpdating)
		app4Out = outLine("4", installationActive)
		app5Out = outLine("5", instanceNotRunning)
		app6Out = outLine("6", deviceService)
		app7Out = outLine("7", installationActive)
		app8Out = outLine("8", deviceUpdating)
		app9Out = outLine("9", instanceUpdating)
	)

	for _, c := range []struct {
		handler         cmd_device.GlobHandler
		globResponses   [][]string
		statusResponses map[string][]interface{}
		gs              cmd_device.GlobSettings
		globPatterns    []string
		expected        string
	}{
		{
			simplePrintHandler,
			allGlobResponses,
			allStatusResponses,
			cmd_device.GlobSettings{},
			allGlobArgs,
			// First installations, then instances, then device services.
			joinLines(app2Out, app4Out, app7Out, app1Out, app3Out, app5Out, app9Out, app6Out, app8Out),
		},
		// TODO(caprita): Test the following cases:
		// Various filters.
		// Parallelism options.
		// No glob arguments.
		// No glob results.
		// Error in glob.
		// Error in status.
		// Error in handler.
	} {
		tapes.rewind()
		var rootTapeResponses []interface{}
		for _, r := range c.globResponses {
			rootTapeResponses = append(rootTapeResponses, GlobResponse{r})
		}
		rootTape.SetResponses(rootTapeResponses...)
		for n, r := range c.statusResponses {
			tapes.forSuffix(n).SetResponses(r...)
		}
		var stdout, stderr bytes.Buffer
		env := &cmdline.Env{Stdout: &stdout, Stderr: &stderr}
		var args []string
		for _, p := range c.globPatterns {
			args = append(args, naming.JoinAddressName(endpoint.String(), p))
		}
		err := cmd_device.Run(ctx, env, args, c.handler, c.gs)
		if err != nil {
			t.Errorf("%v", err)
		}
		if expected, got := c.expected, strings.TrimSpace(stdout.String()); got != expected {
			t.Errorf("Unexpected output. Got:\n%v\nExpected:\n%v\n", got, expected)
		}
	}
}

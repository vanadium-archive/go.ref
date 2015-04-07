// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package exec_test

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"v.io/v23/verror"
	vexec "v.io/x/ref/lib/exec"
)

func TestNoExecProtocol(t *testing.T) {
	cmd := exec.Command("bash", "-c", "printenv")
	stdout, _ := cmd.StdoutPipe()
	ph := vexec.NewParentHandle(cmd, vexec.UseExecProtocolOpt(false))
	if err := ph.Start(); err != nil {
		t.Fatal(err)
	}
	if got, want := ph.WaitForReady(time.Minute), vexec.ErrNotUsingProtocol.ID; verror.ErrorID(got) != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	re := regexp.MustCompile(fmt.Sprintf(".*%s=.*", vexec.ExecVersionVariable))
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			t.Fatalf("%s passed to child", vexec.ExecVersionVariable)
		}
	}
}

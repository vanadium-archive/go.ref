package exec_test

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"testing"
	"time"

	vexec "v.io/x/ref/lib/exec"
	"v.io/x/ref/lib/exec/consts"
)

func TestNoExecProtocol(t *testing.T) {
	cmd := exec.Command("bash", "-c", "printenv")
	stdout, _ := cmd.StdoutPipe()
	ph := vexec.NewParentHandle(cmd, vexec.UseExecProtocolOpt(false))
	if err := ph.Start(); err != nil {
		t.Fatal(err)
	}
	if got, want := ph.WaitForReady(time.Minute), vexec.ErrNotUsingProtocol; got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	re := regexp.MustCompile(fmt.Sprintf(".*%s=.*", consts.ExecVersionVariable))
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			t.Fatalf("%s passed to child", consts.ExecVersionVariable)
		}
	}
}

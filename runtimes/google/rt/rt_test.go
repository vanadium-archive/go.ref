package rt_test

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	_ "veyron/lib/testutil"
	"veyron/lib/testutil/blackbox"

	"veyron2/rt"
	"veyron2/vlog"
)

func init() {
	blackbox.CommandTable["child"] = child
}

func TestHelperProcess(t *testing.T) {
	blackbox.HelperProcess(t)
}

func TestInit(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	l := r.Logger()
	args := fmt.Sprintf("%s", l)
	expected := regexp.MustCompile("name=veyron logdirs=\\[/tmp\\] logtostderr=true|false alsologtostderr=false|true max_stack_buf_size=4292608 v=[0-9] stderrthreshold=2 vmodule= log_backtrace_at=:0")
	if !expected.MatchString(args) {
		t.Errorf("unexpected default args: %q", args)
	}
	if id := r.Identity().PublicID(); id == nil || len(id.Names()) == 0 || len(id.Names()[0]) == 0 {
		t.Errorf("New should have created an identity. Created %v", id)
	}
}

func child(argv []string) {
	r := rt.Init()
	vlog.Infof("%s\n", r.Logger())
	fmt.Printf("%s\n", r.Logger())
	_ = r
	blackbox.WaitForEOFOnStdin()
	fmt.Printf("done\n")
}

func TestInitArgs(t *testing.T) {
	c := blackbox.HelperCommand(t, "child", "--logtostderr=true", "--vv=3", "--", "foobar")
	defer c.Cleanup()
	c.Cmd.Start()
	str, err := c.ReadLineFromChild()
	if err != nil {
		t.Fatalf("failed to read from child: %v", err)
	}
	expected := fmt.Sprintf("name=veyron "+
		"logdirs=[%s] "+
		"logtostderr=true "+
		"alsologtostderr=true "+
		"max_stack_buf_size=4292608 "+
		"v=3 "+
		"stderrthreshold=2 "+
		"vmodule= "+
		"log_backtrace_at=:0",
		os.TempDir())

	if str != expected {
		t.Fatalf("incorrect child output: got %s, expected %s", str, expected)
	}
	c.CloseStdin()
	c.Expect("done")
	c.ExpectEOFAndWait()
}

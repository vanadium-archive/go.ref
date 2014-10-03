package rt_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"testing"

	_ "veyron.io/veyron/veyron/lib/testutil"
	"veyron.io/veyron/veyron/lib/testutil/blackbox"
	irt "veyron.io/veyron/veyron/runtimes/google/rt"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"
)

type context struct {
	local security.Principal
}

func (*context) Method() string                            { return "" }
func (*context) Name() string                              { return "" }
func (*context) Suffix() string                            { return "" }
func (*context) Label() (l security.Label)                 { return }
func (*context) Discharges() map[string]security.Discharge { return nil }
func (*context) LocalID() security.PublicID                { return nil }
func (*context) RemoteID() security.PublicID               { return nil }
func (c *context) LocalPrincipal() security.Principal      { return c.local }
func (*context) LocalBlessings() security.Blessings        { return nil }
func (*context) RemoteBlessings() security.Blessings       { return nil }
func (*context) LocalEndpoint() naming.Endpoint            { return nil }
func (*context) RemoteEndpoint() naming.Endpoint           { return nil }

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

func TestInitPrincipal(t *testing.T) {
	newRT := func() veyron2.Runtime {
		r, err := rt.New()
		if err != nil {
			t.Fatalf("rt.New failed: %v", err)
		}
		return r
	}
	testPrincipal := func(r veyron2.Runtime) security.Principal {
		p := r.Principal()
		if p == nil {
			t.Fatalf("rt.Principal() returned nil")
		}
		blessings := p.BlessingStore().Default()
		if blessings == nil {
			t.Fatalf("rt.Principal().BlessingStore().Default() returned nil")

		}
		if n := len(blessings.ForContext(&context{local: p})); n != 1 {
			t.Fatalf("rt.Principal().BlessingStore().Default() returned Blessing %v with %d recognized blessings, want exactly one recognized blessing", blessings, n)
		}
		return p
	}
	origCredentialsDir := os.Getenv(irt.VeyronCredentialsEnvVar)
	defer os.Setenv(irt.VeyronCredentialsEnvVar, origCredentialsDir)

	// Test that even with VEYRON_CREDENTIALS unset the runtime's Principal
	// is correctly initialized.
	if err := os.Setenv(irt.VeyronCredentialsEnvVar, ""); err != nil {
		t.Fatal(err)
	}
	testPrincipal(newRT())

	// Test that with VEYRON_CREDENTIALS set the runtime's Principal is correctly
	// initialized.
	credentials, err := ioutil.TempDir("", "credentials")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(credentials)
	if err := os.Setenv(irt.VeyronCredentialsEnvVar, credentials); err != nil {
		t.Fatal(err)
	}
	p := testPrincipal(newRT())

	// Mutate the roots and store of this principal.
	blessing, err := p.BlessSelf("irrelevant")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
		t.Fatal(err)
	}
	if err := p.AddToRoots(blessing); err != nil {
		t.Fatal(err)
	}

	// Test that the same principal gets initialized on creating a new runtime
	// from the same credentials directory.
	if got := newRT().Principal(); !reflect.DeepEqual(got, p) {
		t.Fatalf("Initialized Principal: %v, expected: %v", got, p)
	}
}

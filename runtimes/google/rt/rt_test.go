package rt_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/flags/consts"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/testutil"
	vsecurity "veyron.io/veyron/veyron/security"
)

func init() {
	testutil.Init()
	modules.RegisterChild("child", "", child)
	modules.RegisterChild("principal", "", principal)
	modules.RegisterChild("runner", "", runner)
}

func TestHelperProcess(t *testing.T) {
	modules.DispatchInTest()
}

func TestInit(t *testing.T) {
	r, err := rt.New(profileOpt)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	l := r.Logger()
	args := fmt.Sprintf("%s", l)
	expected := regexp.MustCompile("name=veyron logdirs=\\[/tmp\\] logtostderr=true|false alsologtostderr=false|true max_stack_buf_size=4292608 v=[0-9] stderrthreshold=2 vmodule= log_backtrace_at=:0")
	if !expected.MatchString(args) {
		t.Errorf("unexpected default args: %s", args)
	}
	p := r.Principal()
	if p == nil {
		t.Fatalf("A new principal should have been created")
	}
	if p.BlessingStore() == nil {
		t.Fatalf("The principal must have a BlessingStore")
	}
	if p.BlessingStore().Default() == nil {
		t.Errorf("Principal().BlessingStore().Default() should not be nil")
	}
	if p.BlessingStore().ForPeer() == nil {
		t.Errorf("Principal().BlessingStore().ForPeer() should not be nil")
	}
}

func child(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r := rt.Init()
	vlog.Infof("%s\n", r.Logger())
	fmt.Fprintf(stdout, "%s\n", r.Logger())
	modules.WaitForEOF(stdin)
	fmt.Fprintf(stdout, "done\n")
	return nil
}

func TestInitArgs(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start("child", nil, "--logtostderr=true", "--vmodule=*=3", "--", "foobar")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect(fmt.Sprintf("name=veyron "+
		"logdirs=[%s] "+
		"logtostderr=true "+
		"alsologtostderr=true "+
		"max_stack_buf_size=4292608 "+
		"v=0 "+
		"stderrthreshold=2 "+
		"vmodule=*=3 "+
		"log_backtrace_at=:0",
		os.TempDir()))
	h.CloseStdin()
	s.Expect("done")
	s.ExpectEOF()
	h.Shutdown(os.Stderr, os.Stderr)
}

func validatePrincipal(p security.Principal) error {
	if p == nil {
		return fmt.Errorf("nil principal")
	}
	blessings := p.BlessingStore().Default()
	if blessings == nil {
		return fmt.Errorf("rt.Principal().BlessingStore().Default() returned nil")
	}
	ctx := security.NewContext(&security.ContextParams{LocalPrincipal: p})
	if n := len(blessings.ForContext(ctx)); n != 1 {
		fmt.Errorf("rt.Principal().BlessingStore().Default() returned Blessing %v with %d recognized blessings, want exactly one recognized blessing", blessings, n)
	}
	return nil
}

func defaultBlessing(p security.Principal) string {
	return p.BlessingStore().Default().ForContext(security.NewContext(&security.ContextParams{LocalPrincipal: p}))[0]
}

func tmpDir(t *testing.T) string {
	dir, err := ioutil.TempDir("", "rt_test_dir")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	return dir
}

func principal(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r := rt.Init()
	if err := validatePrincipal(r.Principal()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "DEFAULT_BLESSING=%s\n", defaultBlessing(r.Principal()))
	return nil
}

// Runner runs a principal as a subprocess and reports back with its
// own security info and it's childs.
func runner(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r := rt.Init()
	if err := validatePrincipal(r.Principal()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "RUNNER_DEFAULT_BLESSING=%v\n", defaultBlessing(r.Principal()))
	sh, err := modules.NewShell(nil)
	if err != nil {
		return err
	}
	if _, err := sh.Start("principal", nil, args[1:]...); err != nil {
		return err
	}
	// Cleanup copies the output of sh to these Writers.
	sh.Cleanup(stdout, stderr)
	return nil
}

func createCredentialsInDir(t *testing.T, dir string, blessing string) {
	principal, err := vsecurity.CreatePersistentPrincipal(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err := vsecurity.InitDefaultBlessings(principal, blessing); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestPrincipalInheritance(t *testing.T) {
	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer func() {
		sh.Cleanup(os.Stdout, os.Stderr)
	}()

	// Test that the child inherits from the parent's credentials correctly.
	// The running test process may or may not have a credentials directory set
	// up so we have to use a 'runner' process to ensure the correct setup.
	cdir := tmpDir(t)
	defer os.RemoveAll(cdir)

	createCredentialsInDir(t, cdir, "test")

	// directory supplied by the environment.
	credEnv := []string{consts.VeyronCredentials + "=" + cdir}

	h, err := sh.Start("runner", credEnv)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	s := expect.NewSession(t, h.Stdout(), time.Minute)
	runnerBlessing := s.ExpectVar("RUNNER_DEFAULT_BLESSING")
	principalBlessing := s.ExpectVar("DEFAULT_BLESSING")
	if err := s.Error(); err != nil {
		t.Fatalf("failed to read input from children: %s", err)
	}
	h.Shutdown(os.Stdout, os.Stderr)

	wantRunnerBlessing := "test"
	wantPrincipalBlessing := "test/child"
	if runnerBlessing != wantRunnerBlessing || principalBlessing != wantPrincipalBlessing {
		t.Fatalf("unexpected default blessing: got runner %s, principal %s, want runner %s, principal %s", runnerBlessing, principalBlessing, wantRunnerBlessing, wantPrincipalBlessing)
	}

}

func TestPrincipalInit(t *testing.T) {
	// Collect the process' public key and error status
	collect := func(sh *modules.Shell, env []string, args ...string) string {
		h, err := sh.Start("principal", env, args...)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		s := expect.NewSession(t, h.Stdout(), time.Minute)
		s.SetVerbosity(testing.Verbose())
		return s.ExpectVar("DEFAULT_BLESSING")
	}

	// A credentials directory may, or may, not have been already specified.
	// Either way, we want to use our own, so we set it aside and use our own.
	origCredentialsDir := os.Getenv(consts.VeyronCredentials)
	defer os.Setenv(consts.VeyronCredentials, origCredentialsDir)

	// Test that with VEYRON_CREDENTIALS unset the runtime's Principal
	// is correctly initialized.
	if err := os.Setenv(consts.VeyronCredentials, ""); err != nil {
		t.Fatal(err)
	}

	sh, err := modules.NewShell(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	blessing := collect(sh, nil)
	if len(blessing) == 0 {
		t.Fatalf("child returned an empty default blessings set")
	}

	// Test specifying credentials via VEYRON_CREDENTIALS environment.
	cdir1 := tmpDir(t)
	defer os.RemoveAll(cdir1)
	createCredentialsInDir(t, cdir1, "test_env")
	credEnv := []string{consts.VeyronCredentials + "=" + cdir1}

	blessing = collect(sh, credEnv)
	if got, want := blessing, "test_env"; got != want {
		t.Errorf("got default blessings: %q, want %q", got, want)
	}

	// Test specifying credentials via the command line and that the
	// comand line overrides the environment
	cdir2 := tmpDir(t)
	defer os.RemoveAll(cdir2)
	createCredentialsInDir(t, cdir2, "test_cmd")

	blessing = collect(sh, credEnv, "--veyron.credentials="+cdir2)
	if got, want := blessing, "test_cmd"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInitPrincipalFromOption(t *testing.T) {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		t.Fatalf("NewPrincipal() failed: %v", err)
	}
	r, err := rt.New(profileOpt, options.RuntimePrincipal{p})
	if err != nil {
		t.Fatalf("rt.New failed: %v", err)
	}

	if got := r.Principal(); !reflect.DeepEqual(got, p) {
		t.Fatalf("r.Principal(): got %v, want %v", got, p)
	}
}

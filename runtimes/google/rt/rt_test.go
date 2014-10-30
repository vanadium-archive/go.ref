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

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/options"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/vlog"

	"veyron.io/veyron/veyron/lib/expect"
	"veyron.io/veyron/veyron/lib/modules"
	"veyron.io/veyron/veyron/lib/testutil"
	vsecurity "veyron.io/veyron/veyron/security"
)

type context struct {
	local security.Principal
}

// Environment variable pointing to a directory where information about a
// principal (private key, blessing store, blessing roots etc.) is stored.
const veyronCredentialsEnvVar = "VEYRON_CREDENTIALS"

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
	testutil.Init()
	modules.RegisterChild("child", "", child)
	modules.RegisterChild("principal", "", principal)
	modules.RegisterChild("mutate", "", mutatePrincipal)
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
		t.Errorf("unexpected default args: %q", args)
	}
	if id := r.Identity().PublicID(); id == nil || len(id.Names()) == 0 || len(id.Names()[0]) == 0 {
		t.Errorf("New should have created an identity. Created %v", id)
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
	sh := modules.NewShell("child")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start("child", nil, "--logtostderr=true", "--vv=3", "--", "foobar")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect(fmt.Sprintf("name=veyron "+
		"logdirs=[%s] "+
		"logtostderr=true "+
		"alsologtostderr=true "+
		"max_stack_buf_size=4292608 "+
		"v=3 "+
		"stderrthreshold=2 "+
		"vmodule= "+
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
	if n := len(blessings.ForContext(&context{local: p})); n != 1 {
		fmt.Errorf("rt.Principal().BlessingStore().Default() returned Blessing %v with %d recognized blessings, want exactly one recognized blessing", blessings, n)
	}
	return nil
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
	err := validatePrincipal(r.Principal())
	fmt.Fprintf(stdout, "ERROR=%v\n", err)
	fmt.Fprintf(stdout, "PUBKEY=%s\n", r.Principal().PublicKey())
	modules.WaitForEOF(stdin)
	return nil
}

// Runner runs a principal as a subprocess and reports back with its
// own security info and it's childs.
func runner(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r := rt.Init()
	err := validatePrincipal(r.Principal())
	fmt.Fprintf(stdout, "RUNNER_ERROR=%v\n", err)
	fmt.Fprintf(stdout, "RUNNER_PUBKEY=%s\n", r.Principal().PublicKey())
	if err != nil {
		return err
	}
	sh := modules.NewShell("principal")
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start("principal", nil, args[1:]...)
	if err != nil {
		return err
	}
	s := expect.NewSession(nil, h.Stdout(), 1*time.Second) // time.Minute)
	fmt.Fprintf(stdout, s.ReadLine()+"\n")
	fmt.Fprintf(stdout, s.ReadLine()+"\n")
	modules.WaitForEOF(stdin)
	return nil
}

func mutatePrincipal(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	r := rt.Init()

	rtPrincipal := r.Principal()
	err := validatePrincipal(rtPrincipal)
	fmt.Fprintf(stdout, "ERROR=%v\n", err)

	// Mutate the roots and store of this principal.
	blessing, err := rtPrincipal.BlessSelf("irrelevant")
	if err != nil {
		return err
	}
	if _, err := rtPrincipal.BlessingStore().Set(blessing, security.AllPrincipals); err != nil {
		return err
	}
	if err := rtPrincipal.AddToRoots(blessing); err != nil {
		return err
	}
	newRT, err := rt.New(profileOpt)
	if err != nil {
		return fmt.Errorf("rt.New failed: %v", err)
	}
	// Test that the same principal gets initialized on creating a new runtime
	// from the same credentials directory.
	if got := newRT.Principal(); !reflect.DeepEqual(got, rtPrincipal) {
		return fmt.Errorf("Initialized Principal: %v, expected: %v", got.PublicKey(), rtPrincipal.PublicKey())
	}
	fmt.Fprintf(stdout, "PUBKEY=%s\n", newRT.Principal().PublicKey())
	modules.WaitForEOF(stdin)
	return nil
}

func createCredentialsInDir(t *testing.T, dir string) security.Principal {
	principal, err := vsecurity.CreatePersistentPrincipal(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	vsecurity.InitDefaultBlessings(principal, "test")
	return principal
}

func TestPrincipalInheritance(t *testing.T) {
	sh := modules.NewShell("principal", "runner")
	defer func() {
		sh.Cleanup(os.Stdout, os.Stderr)
	}()

	// Test that the child inherits the parent's credentials correctly.
	// The running test process may or may not have a credentials directory set
	// up so we have to use a 'runner' process to ensure the correct setup.
	cdir := tmpDir(t)
	defer os.RemoveAll(cdir)

	principal := createCredentialsInDir(t, cdir)

	// directory supplied by the environment.
	credEnv := []string{veyronCredentialsEnvVar + "=" + cdir}

	h, err := sh.Start("runner", credEnv)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s := expect.NewSession(t, h.Stdout(), 2*time.Second) //time.Minute)
	runnerErr := s.ExpectVar("RUNNER_ERROR")
	runnerPubKey := s.ExpectVar("RUNNER_PUBKEY")
	principalErr := s.ExpectVar("ERROR")
	principalPubKey := s.ExpectVar("PUBKEY")
	if err := s.Error(); err != nil {
		t.Fatalf("failed to read input from children: %s", err)
	}
	h.Shutdown(os.Stdout, os.Stderr)
	if runnerErr != "<nil>" || principalErr != "<nil>" {
		t.Fatalf("unexpected error: runner %q, principal %q", runnerErr, principalErr)
	}
	pubKey := principal.PublicKey().String()
	if runnerPubKey != pubKey || principalPubKey != pubKey {
		t.Fatalf("unexpected pubkeys: expected %s: runner %s, principal %s",
			pubKey, runnerPubKey, principalPubKey)
	}

}

func TestPrincipalInit(t *testing.T) {
	// Collet the process' public key and error status
	collect := func(sh *modules.Shell, cmd string, env []string, args ...string) (string, error) {
		h, err := sh.Start(cmd, env, args...)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		s := expect.NewSession(t, h.Stdout(), time.Minute)
		s.SetVerbosity(testing.Verbose())
		errstr := s.ExpectVar("ERROR")
		pubkey := s.ExpectVar("PUBKEY")
		if errstr != "<nil>" {
			return pubkey, fmt.Errorf("%s", errstr)
		}
		return pubkey, nil
	}

	// A credentials directory may, or may, not have been already specified.
	// Either way, we want to use our own, so we set it aside and use our own.
	origCredentialsDir := os.Getenv(veyronCredentialsEnvVar)
	defer os.Setenv(veyronCredentialsEnvVar, origCredentialsDir)

	// Test that with VEYRON_CREDENTIALS unset the runtime's Principal
	// is correctly initialized.
	if err := os.Setenv(veyronCredentialsEnvVar, ""); err != nil {
		t.Fatal(err)
	}

	sh := modules.NewShell("principal")
	defer sh.Cleanup(os.Stderr, os.Stderr)

	pubkey, err := collect(sh, "principal", nil)
	if err != nil {
		t.Fatalf("child failed to create+validate principal: %v", err)
	}
	if len(pubkey) == 0 {
		t.Fatalf("child failed to return a public key")
	}

	// Test specifying credentials via VEYRON_CREDENTIALS
	cdir1 := tmpDir(t)
	defer os.RemoveAll(cdir1)
	principal := createCredentialsInDir(t, cdir1)
	// directory supplied by the environment.
	credEnv := []string{veyronCredentialsEnvVar + "=" + cdir1}

	pubkey, err = collect(sh, "principal", credEnv)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	if got, want := pubkey, principal.PublicKey().String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Test specifying credentials via the command line and that the
	// comand line overrides the environment
	cdir2 := tmpDir(t)
	defer os.RemoveAll(cdir2)
	clPrincipal := createCredentialsInDir(t, cdir2)

	pubkey, err = collect(sh, "principal", credEnv, "--veyron.credentials="+cdir2)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	if got, want := pubkey, clPrincipal.PublicKey().String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Mutate the roots and store of the principal in the child process.
	pubkey, err = collect(sh, "mutate", credEnv, "--veyron.credentials="+cdir2)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	if got, want := pubkey, clPrincipal.PublicKey().String(); got != want {
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

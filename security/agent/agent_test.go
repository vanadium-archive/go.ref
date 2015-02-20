package agent_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"syscall"
	"testing"
	"time"

	"v.io/core/veyron/lib/expect"
	"v.io/core/veyron/lib/modules"
	"v.io/core/veyron/lib/testutil"
	tsecurity "v.io/core/veyron/lib/testutil/security"
	_ "v.io/core/veyron/profiles"
	"v.io/core/veyron/security/agent"
	"v.io/core/veyron/security/agent/server"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/security"
)

//go:generate v23 test generate

func getPrincipalAndHang(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	p := veyron2.GetPrincipal(ctx)
	fmt.Fprintf(stdout, "DEFAULT_BLESSING=%s\n", p.BlessingStore().Default())
	ioutil.ReadAll(stdin)
	return nil
}

func newAgent(ctx *context.T, sock *os.File, cached bool) (security.Principal, error) {
	fd, err := syscall.Dup(int(sock.Fd()))
	if err != nil {
		return nil, err
	}
	if cached {
		return agent.NewAgentPrincipal(ctx, fd, veyron2.GetClient(ctx))
	} else {
		return agent.NewUncachedPrincipal(ctx, fd, veyron2.GetClient(ctx))
	}
}

func setupAgentPair(t *testing.T, ctx *context.T, p security.Principal) (security.Principal, security.Principal) {
	sock, err := server.RunAnonymousAgent(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()

	agent1, err := newAgent(ctx, sock, true)
	if err != nil {
		t.Fatal(err)
	}
	agent2, err := newAgent(ctx, sock, true)
	if err != nil {
		t.Fatal(err)
	}

	return agent1, agent2
}

func setupAgent(ctx *context.T, caching bool) security.Principal {
	sock, err := server.RunAnonymousAgent(ctx, veyron2.GetPrincipal(ctx))
	if err != nil {
		panic(err)
	}
	defer sock.Close()
	agent, err := newAgent(ctx, sock, caching)
	if err != nil {
		panic(err)
	}
	return agent
}

func TestAgent(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()

	var (
		p              = tsecurity.NewPrincipal("agentTest")
		agent1, agent2 = setupAgentPair(t, ctx, p)
	)

	defP, def1, def2 := p.BlessingStore().Default(), agent1.BlessingStore().Default(), agent2.BlessingStore().Default()

	if !reflect.DeepEqual(defP, def1) {
		t.Errorf("Default blessing mismatch. Wanted %v, got %v", defP, def1)
	}
	if !reflect.DeepEqual(defP, def2) {
		t.Errorf("Default blessing mismatch. Wanted %v, got %v", defP, def2)
	}

	// Check that we're caching:
	// Modify the principal directly and the client's shouldn't notic.
	blessing, err := p.BlessSelf("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.BlessingStore().SetDefault(blessing); err != nil {
		t.Fatal(err)
	}
	def1, def2 = agent1.BlessingStore().Default(), agent2.BlessingStore().Default()
	if !reflect.DeepEqual(defP, def1) {
		t.Errorf("Default blessing mismatch. Wanted %v, got %v", defP, def1)
	}
	if !reflect.DeepEqual(defP, def2) {
		t.Errorf("Default blessing mismatch. Wanted %v, got %v", defP, def2)
	}

	// Now make a change through the client.
	blessing, err = agent1.BlessSelf("john")
	if err != nil {
		t.Fatal(err)
	}
	if err = agent2.BlessingStore().SetDefault(blessing); err != nil {
		t.Fatal(err)
	}
	// The principal and the other client should both reflect the change.
	newDefault := p.BlessingStore().Default()
	if !reflect.DeepEqual(newDefault, blessing) {
		t.Errorf("Default blessing mismatch. Wanted %v, got %v", blessing, newDefault)
	}
	// There's no synchronization, so keep fetching from the other client.
	// Eventually it should get notified of the new value.
	for i := 0; i < 10000 && !reflect.DeepEqual(blessing, agent1.BlessingStore().Default()); i += 1 {
		time.Sleep(100 * time.Millisecond)
	}

	if !reflect.DeepEqual(agent1.BlessingStore().Default(), blessing) {
		t.Errorf("Default blessing mismatch. Wanted %v, got %v", blessing, agent1.BlessingStore().Default())
	}
}

func TestAgentShutdown(t *testing.T) {
	ctx, shutdown := testutil.InitForTest()

	// This starts an agent
	sh, err := modules.NewShell(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The child process will connect to the agent
	h, err := sh.Start("getPrincipalAndHang", nil)
	if err != nil {
		t.Fatal(err)
	}

	s := expect.NewSession(t, h.Stdout(), time.Minute)
	fmt.Fprintf(os.Stderr, "reading var...\n")
	s.ExpectVar("DEFAULT_BLESSING")
	fmt.Fprintf(os.Stderr, "read\n")
	if err := s.Error(); err != nil {
		t.Fatalf("failed to read input: %s", err)
	}
	fmt.Fprintf(os.Stderr, "shutting down...\n")
	// This shouldn't not hang
	shutdown()
	fmt.Fprintf(os.Stderr, "shut down\n")

	fmt.Fprintf(os.Stderr, "cleanup...\n")
	sh.Cleanup(os.Stdout, os.Stderr)
	fmt.Fprintf(os.Stderr, "cleanup done\n")
}

var message = []byte("bugs bunny")

func runSignBenchmark(b *testing.B, p security.Principal) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Sign(message); err != nil {
			b.Fatal(err)
		}
	}
}

func runDefaultBenchmark(b *testing.B, p security.Principal) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if d := p.BlessingStore().Default(); d == nil {
			b.Fatal("nil")
		}
	}
}

func runRecognizedNegativeBenchmark(b *testing.B, p security.Principal) {
	key := p.PublicKey()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if d := p.Roots().Recognized(key, "foobar"); d == nil {
			b.Fatal("nil")
		}
	}
}

func runRecognizedBenchmark(b *testing.B, p security.Principal) {
	key := p.PublicKey()
	blessing, err := p.BlessSelf("foobar")
	if err != nil {
		b.Fatal(err)
	}
	err = p.AddToRoots(blessing)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if d := p.Roots().Recognized(key, "foobar"); d != nil {
			b.Fatal(d)
		}
	}
}

func BenchmarkSignNoAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runSignBenchmark(b, veyron2.GetPrincipal(ctx))
}

func BenchmarkSignCachedAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runSignBenchmark(b, setupAgent(ctx, true))
}

func BenchmarkSignUncachedAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runSignBenchmark(b, setupAgent(ctx, false))
}

func BenchmarkDefaultNoAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runDefaultBenchmark(b, veyron2.GetPrincipal(ctx))
}

func BenchmarkDefaultCachedAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runDefaultBenchmark(b, setupAgent(ctx, true))
}

func BenchmarkDefaultUncachedAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runDefaultBenchmark(b, setupAgent(ctx, false))
}

func BenchmarkRecognizedNegativeNoAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runRecognizedNegativeBenchmark(b, veyron2.GetPrincipal(ctx))
}

func BenchmarkRecognizedNegativeCachedAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runRecognizedNegativeBenchmark(b, setupAgent(ctx, true))
}

func BenchmarkRecognizedNegativeUncachedAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runRecognizedNegativeBenchmark(b, setupAgent(ctx, false))
}

func BenchmarkRecognizedNoAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runRecognizedBenchmark(b, veyron2.GetPrincipal(ctx))
}

func BenchmarkRecognizedCachedAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runRecognizedBenchmark(b, setupAgent(ctx, true))
}

func BenchmarkRecognizedUncachedAgent(b *testing.B) {
	ctx, shutdown := testutil.InitForTest()
	defer shutdown()
	runRecognizedBenchmark(b, setupAgent(ctx, false))
}

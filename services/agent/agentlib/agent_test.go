// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agentlib_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/x/ref/services/agent/agentlib"
	"v.io/x/ref/services/agent/internal/server"
	"v.io/x/ref/test"
	"v.io/x/ref/test/modules"
	"v.io/x/ref/test/testutil"

	_ "v.io/x/ref/runtime/factories/generic"
)

// As of April 28, 2015, the benchmarks for serving a principal with and
// without the agent are as follows:
//
// BenchmarkSignNoAgent                    :  889608 ns/op
// BenchmarkSignCachedAgent                : 6961410 ns/op
// BenchmarkSignUncachedAgent	           : 7403763 ns/op
// BenchmarkDefaultNoAgent	           :     139 ns/op
// BenchmarkDefaultCachedAgent	           :      41 ns/op
// BenchmarkDefaultUncachedAgent	   : 9732978 ns/op
// BenchmarkRecognizedNegativeNoAgent	   :   34859 ns/op
// BenchmarkRecognizedNegativeCachedAgent  :   31043 ns/op
// BenchmarkRecognizedNegativeUncachedAgent: 5110308 ns/op
// BenchmarkRecognizedNoAgent	           :   13457 ns/op
// BenchmarkRecognizedCachedAgent	   :   12609 ns/op
// BenchmarkRecognizedUncachedAgent	   : 4232959 ns/op

//go:generate v23 test generate

var getPrincipalAndHang = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	p := v23.GetPrincipal(ctx)
	fmt.Fprintf(env.Stdout, "DEFAULT_BLESSING=%s\n", p.BlessingStore().Default())
	ioutil.ReadAll(env.Stdin)
	return nil
}, "getPrincipalAndHang")

func newAgent(ctx *context.T, endpoint string, cached bool) (security.Principal, error) {
	ep, err := v23.NewEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if cached {
		return agentlib.NewAgentPrincipal(ctx, ep, v23.GetClient(ctx))
	} else {
		return agentlib.NewUncachedPrincipal(ctx, ep, v23.GetClient(ctx))
	}
}

func setupAgentPair(t *testing.T, ctx *context.T, p security.Principal) (security.Principal, security.Principal) {
	sock, ep, err := server.RunAnonymousAgent(ctx, p, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()

	agent1, err := newAgent(ctx, ep, true)
	if err != nil {
		t.Fatal(err)
	}
	agent2, err := newAgent(ctx, ep, true)
	if err != nil {
		t.Fatal(err)
	}

	return agent1, agent2
}

func setupAgent(ctx *context.T, caching bool) security.Principal {
	sock, ep, err := server.RunAnonymousAgent(ctx, v23.GetPrincipal(ctx), -1)
	if err != nil {
		panic(err)
	}
	defer sock.Close()
	agent, err := newAgent(ctx, ep, caching)
	if err != nil {
		panic(err)
	}
	return agent
}

func TestAgent(t *testing.T) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()

	var (
		p              = testutil.NewPrincipal("agentTest")
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
	ctx, shutdown := test.InitForTest()

	// This starts an agent
	sh, err := modules.NewShell(ctx, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatal(err)
	}
	// The child process will connect to the agent
	h, err := sh.Start(nil, getPrincipalAndHang)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Fprintf(os.Stderr, "reading var...\n")
	h.ExpectVar("DEFAULT_BLESSING")
	fmt.Fprintf(os.Stderr, "read\n")
	if err := h.Error(); err != nil {
		t.Fatalf("failed to read input: %s", err)
	}
	fmt.Fprintf(os.Stderr, "shutting down...\n")
	// This should not hang
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
		if d := p.BlessingStore().Default(); d.IsZero() {
			b.Fatal("empty blessings")
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
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runSignBenchmark(b, v23.GetPrincipal(ctx))
}

func BenchmarkSignCachedAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runSignBenchmark(b, setupAgent(ctx, true))
}

func BenchmarkSignUncachedAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runSignBenchmark(b, setupAgent(ctx, false))
}

func BenchmarkDefaultNoAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runDefaultBenchmark(b, v23.GetPrincipal(ctx))
}

func BenchmarkDefaultCachedAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runDefaultBenchmark(b, setupAgent(ctx, true))
}

func BenchmarkDefaultUncachedAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runDefaultBenchmark(b, setupAgent(ctx, false))
}

func BenchmarkRecognizedNegativeNoAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runRecognizedNegativeBenchmark(b, v23.GetPrincipal(ctx))
}

func BenchmarkRecognizedNegativeCachedAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runRecognizedNegativeBenchmark(b, setupAgent(ctx, true))
}

func BenchmarkRecognizedNegativeUncachedAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runRecognizedNegativeBenchmark(b, setupAgent(ctx, false))
}

func BenchmarkRecognizedNoAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runRecognizedBenchmark(b, v23.GetPrincipal(ctx))
}

func BenchmarkRecognizedCachedAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runRecognizedBenchmark(b, setupAgent(ctx, true))
}

func BenchmarkRecognizedUncachedAgent(b *testing.B) {
	ctx, shutdown := test.InitForTest()
	defer shutdown()
	runRecognizedBenchmark(b, setupAgent(ctx, false))
}

package security

// This is a performance benchmark that tests the performance of the
// chain and tree Identity implementations.
//
// Below are the results obtained on running the benchmark tests on April 2, 2014
// on a desktop machine with a 12 core Intel Xeon E5-1650 @ 3.20GHz processor,
// clock speed of 1200Mhz, and 32GB RAM.
//
// -- chain implementation --
//
// BenchmarkNewChain                    1337091 ns/op
// BenchmarkBlessChain                   773872 ns/op
// BenchmarkEncode0BlessingChain          47661 ns/op
// BenchmarkEncode1BlessingChain          52063 ns/op
// BenchmarkDecode0BlessingChain        2594072 ns/op
// BenchmarkDecode1BlessingChain        5092197 ns/op
//
// Wire size with 0 blessings: 676 bytes ("untrusted/X")
// Wire size with 1 blessings: 976 bytes ("untrusted/X/X")
// Wire size with 2 blessings: 1275 bytes ("untrusted/X/X/X")
import (
	"testing"
	"time"

	"veyron2/security"
)

func benchmarkBless(b *testing.B, blesser security.PrivateID, blessee security.PublicID) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := blesser.Bless(blessee, "friend_bob", 1*time.Second, nil); err != nil {
			b.Fatal(err)
		}
	}

}

func benchmarkEncode(b *testing.B, id security.PublicID) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := encode(id); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkDecode(b *testing.B, idBytes []byte) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decode(idBytes); err != nil {
			b.Fatal(err)
		}
	}
}

// -- chain implementation benchmarks --

func BenchmarkNewChain(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := newChainPrivateID("X", nil); err != nil {
			b.Fatalf("Failed newChainPrivateID #%d: %v", i, err)
		}

	}
}

func BenchmarkBlessChain(b *testing.B) {
	benchmarkBless(b, newChain("alice"), newChain("bob").PublicID())
}
func BenchmarkEncode0BlessingChain(b *testing.B) {
	benchmarkEncode(b, newChain("alice").PublicID())
}
func BenchmarkEncode1BlessingChain(b *testing.B) {
	benchmarkEncode(b, bless(newChain("immaterial").PublicID(), veyronChain, "alice", nil))
}

func BenchmarkDecode0BlessingChain(b *testing.B) {
	idBytes, err := encode(newChain("alice").PublicID())
	if err != nil {
		b.Fatal(err)
	}
	benchmarkDecode(b, idBytes)
}

func BenchmarkDecode1BlessingChain(b *testing.B) {
	idBytes, err := encode(bless(newChain("immaterial").PublicID(), veyronChain, "alice", nil))
	if err != nil {
		b.Fatal(err)
	}
	benchmarkDecode(b, idBytes)
}

func TestChainWireSize(t *testing.T) {
	const N = 3
	priv := newChain("X")
	for i := 0; i < N; i++ {
		pub := priv.PublicID()
		buf, err := encode(pub)
		if err != nil {
			t.Fatalf("Failed to encode %q: %v", pub, err)
		}
		t.Logf("Wire size of %T with %d blessings: %d bytes (%q)", pub, i, len(buf), pub)
		priv = derive(bless(pub, priv, "X", nil), priv)
	}
}

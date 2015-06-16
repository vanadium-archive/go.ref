// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crypto

import (
	"bytes"
	"crypto/rand"
	"net"
	"testing"
	"testing/quick"

	"v.io/x/ref/runtime/internal/lib/iobuf"
)

func quickTest(t *testing.T, e Encrypter, d Decrypter) {
	f := func(plaintext []byte) bool {
		plainslice := iobuf.NewSlice(plaintext)
		cipherslice, err := e.Encrypt(plainslice)
		if err != nil {
			t.Error(err)
			return false
		}
		plainslice, err = d.Decrypt(cipherslice)
		if err != nil {
			t.Error(err)
			return false
		}
		defer plainslice.Release()
		return bytes.Equal(plainslice.Contents, plaintext)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func TestNull(t *testing.T) {
	crypter := NewNullCrypter()
	quickTest(t, crypter, crypter)
	crypter.String() // Only to test that String does not crash.
}

func TestNullWithChannelBinding(t *testing.T) {
	cb := []byte{1, 2, 3, 5}
	crypter := NewNullCrypterWithChannelBinding(cb)
	quickTest(t, crypter, crypter)
	if got := crypter.ChannelBinding(); !bytes.Equal(cb, got) {
		t.Errorf("Unexpected channel binding; got %q, want %q", got, cb)
	}
	crypter.String() // Only to test that String does not crash.
}

func testSimple(t *testing.T, c1, c2 Crypter) {
	// Execute String just to check that it does not crash.
	c1.String()
	c2.String()
	if t.Failed() {
		return
	}
	quickTest(t, c1, c2)
	quickTest(t, c2, c1)

	// Log the byte overhead of encryption, just so that test output has a
	// record.
	var overhead [10]int
	for i := 0; i < len(overhead); i++ {
		size := 1 << uint(i)
		slice, err := c1.Encrypt(iobuf.NewSlice(make([]byte, size)))
		overhead[i] = slice.Size() - size
		slice.Release()
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	t.Logf("Byte overhead of encryption: %v", overhead)
}

func TestBox(t *testing.T) {
	c1, c2 := boxCrypters(t, nil, nil)
	testSimple(t, c1, c2)
}

// testChannelBinding attempts to ensure that:
// (a) ChannelBinding returns the same value for both ends of a Crypter
// (b) ChannelBindings are unique
// For (b), we simply test many times and check that no two instances have the same ChannelBinding value.
// Yes, this test isn't exhaustive. If you have ideas, please share.
func testChannelBinding(t *testing.T, factory func(testing.TB, net.Conn, net.Conn) (Crypter, Crypter)) {
	values := make([][]byte, 100)
	for i := 0; i < len(values); i++ {
		conn1, conn2 := net.Pipe()
		c1, c2 := factory(t, conn1, conn2)
		if !bytes.Equal(c1.ChannelBinding(), c2.ChannelBinding()) {
			t.Fatalf("Two ends of the crypter ended up with different channel bindings (iteration #%d)", i)
		}
		values[i] = c1.ChannelBinding()
	}
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if bytes.Equal(values[i], values[j]) {
				t.Fatalf("Same ChannelBinding seen on multiple channels (%d and %d)", i, j)
			}
		}
	}
}

func TestChannelBindingBox(t *testing.T) { testChannelBinding(t, boxCrypters) }

type factory func(t testing.TB, server, client net.Conn) (Crypter, Crypter)

func boxCrypters(t testing.TB, _, _ net.Conn) (Crypter, Crypter) {
	server, client := make(chan *BoxKey, 1), make(chan *BoxKey, 1)
	clientExchanger := func(pubKey *BoxKey) (*BoxKey, error) {
		client <- pubKey
		return <-server, nil
	}
	serverExchanger := func(pubKey *BoxKey) (*BoxKey, error) {
		server <- pubKey
		return <-client, nil
	}
	crypters := make(chan Crypter)
	for _, ex := range []BoxKeyExchanger{clientExchanger, serverExchanger} {
		go func(exchanger BoxKeyExchanger) {
			crypter, err := NewBoxCrypter(exchanger, iobuf.NewAllocator(iobuf.NewPool(0), 0))
			if err != nil {
				t.Fatal(err)
			}
			crypters <- crypter
		}(ex)
	}
	return <-crypters, <-crypters
}

func benchmarkEncrypt(b *testing.B, crypters factory, size int) {
	plaintext := make([]byte, size)
	if _, err := rand.Read(plaintext); err != nil {
		b.Fatal(err)
	}
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()
	e, _ := crypters(b, conn1, conn2)
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cipher, err := e.Encrypt(iobuf.NewSlice(plaintext))
		if err != nil {
			b.Fatal(err)
		}
		cipher.Release()
	}
}

func BenchmarkBoxEncrypt_1B(b *testing.B)  { benchmarkEncrypt(b, boxCrypters, 1) }
func BenchmarkBoxEncrypt_1K(b *testing.B)  { benchmarkEncrypt(b, boxCrypters, 1<<10) }
func BenchmarkBoxEncrypt_10K(b *testing.B) { benchmarkEncrypt(b, boxCrypters, 10<<10) }
func BenchmarkBoxEncrypt_1M(b *testing.B)  { benchmarkEncrypt(b, boxCrypters, 1<<20) }
func BenchmarkBoxEncrypt_5M(b *testing.B)  { benchmarkEncrypt(b, boxCrypters, 5<<20) }

func benchmarkRoundTrip(b *testing.B, crypters factory, size int) {
	plaintext := make([]byte, size)
	if _, err := rand.Read(plaintext); err != nil {
		b.Fatal(err)
	}
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()
	e, d := crypters(b, conn1, conn2)
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cipherslice, err := e.Encrypt(iobuf.NewSlice(plaintext))
		if err != nil {
			b.Fatal(err)
		}
		plainslice, err := d.Decrypt(cipherslice)
		if err != nil {
			b.Fatal(err)
		}
		plainslice.Release()
	}
}

func BenchmarkBoxRoundTrip_1B(b *testing.B)  { benchmarkRoundTrip(b, boxCrypters, 1) }
func BenchmarkBoxRoundTrip_1K(b *testing.B)  { benchmarkRoundTrip(b, boxCrypters, 1<<10) }
func BenchmarkBoxRoundTrip_10K(b *testing.B) { benchmarkRoundTrip(b, boxCrypters, 10<<10) }
func BenchmarkBoxRoundTrip_1M(b *testing.B)  { benchmarkRoundTrip(b, boxCrypters, 1<<20) }
func BenchmarkBoxRoundTrip_5M(b *testing.B)  { benchmarkRoundTrip(b, boxCrypters, 5<<20) }

func benchmarkSetup(b *testing.B, crypters factory) {
	for i := 0; i < b.N; i++ {
		conn1, conn2 := net.Pipe()
		crypters(b, conn1, conn2)
		conn1.Close()
		conn2.Close()
	}
}

func BenchmarkBoxSetup(b *testing.B) { benchmarkSetup(b, boxCrypters) }

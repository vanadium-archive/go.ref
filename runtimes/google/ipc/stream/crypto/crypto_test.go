package crypto

import (
	"bytes"
	"crypto/rand"
	"net"
	"testing"
	"testing/quick"

	"veyron/runtimes/google/lib/iobuf"
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

func TestTLS(t *testing.T) {
	c1, c2 := tlsCrypters(t)
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

func TestTLSNil(t *testing.T) {
	c1, c2 := tlsCrypters(t)
	if t.Failed() {
		return
	}
	cipher, err := c1.Encrypt(iobuf.NewSlice(nil))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c2.Decrypt(cipher)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Size() != 0 {
		t.Fatalf("Decryption produced non-empty data (%d)", plain.Size())
	}
}

func TestTLSFragmentedPlaintext(t *testing.T) {
	// Form RFC 5246, Section 6.2.1, the maximum length of a TLS record is
	// 16K (it is represented by a uint16).
	// http://tools.ietf.org/html/rfc5246#section-6.2.1
	const dataLen = 16384 + 1
	enc, dec := tlsCrypters(t)
	cipher, err := enc.Encrypt(iobuf.NewSlice(make([]byte, dataLen)))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := dec.Decrypt(cipher)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain.Contents, make([]byte, dataLen)) {
		t.Errorf("Got %d bytes, want %d bytes of zeroes", plain.Size(), dataLen)
	}
}

func tlsCrypters(t testing.TB) (Crypter, Crypter) {
	serverConn, clientConn := net.Pipe()
	crypters := make(chan Crypter)
	go func() {
		server, err := NewTLSServer(serverConn, iobuf.NewPool(0))
		if err != nil {
			t.Fatal(err)
		}
		crypters <- server
	}()

	go func() {
		client, err := NewTLSClient(clientConn, nil, iobuf.NewPool(0))
		if err != nil {
			t.Fatal(err)
		}
		crypters <- client
	}()
	c1 := <-crypters
	c2 := <-crypters
	return c1, c2
}

func benchmarkEncrypt(b *testing.B, size int) {
	plaintext := make([]byte, size)
	if _, err := rand.Read(plaintext); err != nil {
		b.Fatal(err)
	}
	e, _ := tlsCrypters(b)
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

func BenchmarkEncrypt_1B(b *testing.B)  { benchmarkEncrypt(b, 1) }
func BenchmarkEncrypt_1K(b *testing.B)  { benchmarkEncrypt(b, 1<<10) }
func BenchmarkEncrypt_10K(b *testing.B) { benchmarkEncrypt(b, 10<<10) }
func BenchmarkEncrypt_1M(b *testing.B)  { benchmarkEncrypt(b, 1<<20) }
func BenchmarkEncrypt_5M(b *testing.B)  { benchmarkEncrypt(b, 5<<20) }

func benchmarkRoundTrip(b *testing.B, size int) {
	plaintext := make([]byte, size)
	if _, err := rand.Read(plaintext); err != nil {
		b.Fatal(err)
	}
	e, d := tlsCrypters(b)
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
func BenchmarkRoundTrip_1B(b *testing.B)  { benchmarkRoundTrip(b, 1) }
func BenchmarkRoundTrip_1K(b *testing.B)  { benchmarkRoundTrip(b, 1<<10) }
func BenchmarkRoundTrip_10K(b *testing.B) { benchmarkRoundTrip(b, 10<<10) }
func BenchmarkRoundTrip_1M(b *testing.B)  { benchmarkRoundTrip(b, 1<<20) }
func BenchmarkRoundTrip_5M(b *testing.B)  { benchmarkRoundTrip(b, 5<<20) }

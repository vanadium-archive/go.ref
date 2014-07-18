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

func TestTLS(t *testing.T) {
	server, client := net.Pipe()
	c1, c2 := tlsCrypters(t, server, client)
	testSimple(t, c1, c2)
}

func TestBox(t *testing.T) {
	server, client := net.Pipe()
	c1, c2 := boxCrypters(t, server, client)
	testSimple(t, c1, c2)
}

func TestTLSNil(t *testing.T) {
	conn1, conn2 := net.Pipe()
	c1, c2 := tlsCrypters(t, conn1, conn2)
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
	conn1, conn2 := net.Pipe()
	enc, dec := tlsCrypters(t, conn1, conn2)
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

type factory func(t testing.TB, server, client net.Conn) (Crypter, Crypter)

func tlsCrypters(t testing.TB, serverConn, clientConn net.Conn) (Crypter, Crypter) {
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

func boxCrypters(t testing.TB, serverConn, clientConn net.Conn) (Crypter, Crypter) {
	crypters := make(chan Crypter)
	for _, conn := range []net.Conn{serverConn, clientConn} {
		go func(conn net.Conn) {
			crypter, err := NewBoxCrypter(conn, iobuf.NewPool(0))
			if err != nil {
				t.Fatal(err)
			}
			crypters <- crypter
		}(conn)
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

func BenchmarkTLSEncrypt_1B(b *testing.B)  { benchmarkEncrypt(b, tlsCrypters, 1) }
func BenchmarkTLSEncrypt_1K(b *testing.B)  { benchmarkEncrypt(b, tlsCrypters, 1<<10) }
func BenchmarkTLSEncrypt_10K(b *testing.B) { benchmarkEncrypt(b, tlsCrypters, 10<<10) }
func BenchmarkTLSEncrypt_1M(b *testing.B)  { benchmarkEncrypt(b, tlsCrypters, 1<<20) }
func BenchmarkTLSEncrypt_5M(b *testing.B)  { benchmarkEncrypt(b, tlsCrypters, 5<<20) }

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
func BenchmarkTLSRoundTrip_1B(b *testing.B)  { benchmarkRoundTrip(b, tlsCrypters, 1) }
func BenchmarkTLSRoundTrip_1K(b *testing.B)  { benchmarkRoundTrip(b, tlsCrypters, 1<<10) }
func BenchmarkTLSRoundTrip_10K(b *testing.B) { benchmarkRoundTrip(b, tlsCrypters, 10<<10) }
func BenchmarkTLSRoundTrip_1M(b *testing.B)  { benchmarkRoundTrip(b, tlsCrypters, 1<<20) }
func BenchmarkTLSRoundTrip_5M(b *testing.B)  { benchmarkRoundTrip(b, tlsCrypters, 5<<20) }

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

func BenchmarkTLSSetup(b *testing.B) { benchmarkSetup(b, tlsCrypters) }
func BenchmarkBoxSetup(b *testing.B) { benchmarkSetup(b, boxCrypters) }

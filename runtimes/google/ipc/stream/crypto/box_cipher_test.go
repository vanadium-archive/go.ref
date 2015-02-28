package crypto_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/nacl/box"

	"v.io/x/ref/runtimes/google/ipc/stream/crypto"
)

// Add space for a MAC.
func newMessage(s string) []byte {
	b := make([]byte, len(s)+box.Overhead)
	copy(b, []byte(s))
	return b
}

func TestOpenSeal(t *testing.T) {
	pub1, pvt1, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("can't generate key")
	}
	pub2, pvt2, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("can't generate key")
	}
	c1 := crypto.NewControlCipherIPC6(pub2, pvt1, true)
	c2 := crypto.NewControlCipherIPC6(pub1, pvt2, false)

	msg1 := newMessage("hello")
	if err := c1.Seal(msg1); err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	msg2 := newMessage("world")
	if err := c1.Seal(msg2); err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	msg3 := newMessage("hello")
	if err := c1.Seal(msg3); err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if bytes.Compare(msg1, msg3) == 0 {
		t.Errorf("message should differ: %q, %q", msg1, msg3)
	}

	// Check that the client does not encrypt the same.
	msg4 := newMessage("hello")
	if err := c2.Seal(msg4); err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if bytes.Compare(msg4, msg1) == 0 {
		t.Errorf("messages should differ %q vs. %q", msg4, msg1)
	}

	// Corrupted message should not decrypt.
	msg1[0] ^= 1
	if ok := c2.Open(msg1); ok {
		t.Errorf("expected error")
	}

	// Fix the message and try again.
	msg1[0] ^= 1
	if ok := c2.Open(msg1); !ok {
		t.Errorf("Open failed")
	}
	if bytes.Compare(msg1[:5], []byte("hello")) != 0 {
		t.Errorf("got %q, expected %q", msg1[:5], "hello")
	}

	// msg3 should not decrypt.
	if ok := c2.Open(msg3); ok {
		t.Errorf("expected error")
	}

	// Resume.
	if ok := c2.Open(msg2); !ok {
		t.Errorf("Open failed")
	}
	if bytes.Compare(msg2[:5], []byte("world")) != 0 {
		t.Errorf("got %q, expected %q", msg2[:5], "world")
	}
	if ok := c2.Open(msg3); !ok {
		t.Errorf("Open failed")
	}
	if bytes.Compare(msg3[:5], []byte("hello")) != 0 {
		t.Errorf("got %q, expected %q", msg3[:5], "hello")
	}
}

func TestXORKeyStream(t *testing.T) {
	pub1, pvt1, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("can't generate key")
	}
	pub2, pvt2, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("can't generate key")
	}
	c1 := crypto.NewControlCipherIPC6(pub2, pvt1, true)
	c2 := crypto.NewControlCipherIPC6(pub1, pvt2, false)

	msg1 := []byte("hello")
	msg2 := []byte("world")
	msg3 := []byte("hello")
	c1.Encrypt(msg1)
	c1.Encrypt(msg2)
	c1.Encrypt(msg3)
	if bytes.Compare(msg1, msg3) == 0 {
		t.Errorf("messages should differ: %q, %q", msg1, msg3)
	}

	c2.Decrypt(msg1)
	c2.Decrypt(msg2)
	c2.Decrypt(msg3)
	s1 := string(msg1)
	s2 := string(msg2)
	s3 := string(msg3)
	if s1 != "hello" {
		t.Errorf("got %q, expected 'hello'", s1)
	}
	if s2 != "world" {
		t.Errorf("got %q, expected 'world'", s2)
	}
	if s3 != "hello" {
		t.Errorf("got %q, expected 'hello'", s3)
	}
}

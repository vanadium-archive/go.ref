package util

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestMacaroon(t *testing.T) {
	key := randBytes(t)
	incorrectKey := randBytes(t)
	input := randBytes(t)

	m := NewMacaroon(key, input)

	// Test incorrect key.
	decoded, err := m.Decode(incorrectKey)
	if err == nil {
		t.Errorf("m.Decode should have failed")
	}
	if decoded != nil {
		t.Errorf("decoded value should be nil when decode fails")
	}

	// Test correct key.
	if decoded, err = m.Decode(key); err != nil {
		t.Errorf("m.Decode should have succeeded")
	}
	if !bytes.Equal(decoded, input) {
		t.Errorf("decoded value should equal input")
	}
}

func randBytes(t *testing.T) []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("bytes creation failed: %v", err)
	}
	return b
}

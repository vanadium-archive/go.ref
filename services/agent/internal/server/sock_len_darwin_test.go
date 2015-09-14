package server

import (
	"testing"
)

func TestMaxSockPathLen(t *testing.T) {
	if length := GetMaxSockPathLen(); length != 104 {
		t.Errorf("Expected max socket path length to be 104 on darwin, got %d instead", length)
	}
}

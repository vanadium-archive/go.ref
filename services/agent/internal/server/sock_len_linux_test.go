package server

import (
	"testing"
)

func TestMaxSockPathLen(t *testing.T) {
	if length := GetMaxSockPathLen(); length != 108 {
		t.Errorf("Expected max socket path length to be 108 on linux, got %d instead", length)
	}
}

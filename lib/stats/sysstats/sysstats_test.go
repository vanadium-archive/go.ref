package sysstats_test

import (
	"os"
	"testing"

	"v.io/x/ref/lib/stats"
	_ "v.io/x/ref/lib/stats/sysstats"
)

func TestHostname(t *testing.T) {
	obj, err := stats.GetStatsObject("system/hostname")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, err := os.Hostname()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := obj.Value(); got != expected {
		t.Errorf("unexpected result. Got %q, want %q", got, expected)
	}
}

func TestMemStats(t *testing.T) {
	alloc, err := stats.GetStatsObject("system/memstats/Alloc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v := alloc.Value(); v == uint64(0) {
		t.Errorf("unexpected Alloc value. Got %v, want != 0", v)
	}
}

func TestPid(t *testing.T) {
	obj, err := stats.GetStatsObject("system/pid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := int64(os.Getpid())
	if got := obj.Value(); got != expected {
		t.Errorf("unexpected result. Got %q, want %q", got, expected)
	}
}

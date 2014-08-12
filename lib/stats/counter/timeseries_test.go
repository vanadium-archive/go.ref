package counter

import (
	"testing"
	"time"
)

func TestTimeSeries(t *testing.T) {
	now := time.Unix(1, 0)
	ts := newTimeSeries(now, 5*time.Second, time.Second)

	// Time 1
	ts.advanceTime(now)
	ts.set(123)
	if expected, got := int64(123), ts.headValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	ts.set(234)
	if expected, got := int64(234), ts.headValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(234), ts.min(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(234), ts.max(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}

	// Time 2
	now = now.Add(time.Second)
	ts.advanceTime(now)
	ts.set(345)
	if expected, got := int64(345), ts.headValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(234), ts.tailValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(234), ts.min(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(345), ts.max(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}

	// Time 4
	now = now.Add(2 * time.Second)
	ts.advanceTime(now)
	ts.set(111)
	if expected, got := int64(111), ts.headValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(234), ts.tailValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(111), ts.min(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(345), ts.max(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}

	// Time 7
	now = now.Add(3 * time.Second)
	ts.advanceTime(now)
	if expected, got := int64(111), ts.headValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(345), ts.tailValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}

	// Time 27
	now = now.Add(20 * time.Second)
	ts.advanceTime(now)
	if expected, got := int64(111), ts.headValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
	if expected, got := int64(111), ts.tailValue(); got != expected {
		t.Errorf("unexpected value. Got %v, want %v", got, expected)
	}
}

func TestTimeSeriesRate(t *testing.T) {
	now := time.Unix(1, 0)
	ts := newTimeSeries(now, 60*time.Second, time.Second)
	// Increment by 5 every 4 seconds. The rate is 1.25.
	for i := 0; i < 10; i++ {
		now = now.Add(4 * time.Second)
		ts.advanceTime(now)
		ts.incr(5)
	}
	if expected, got := float64(1.25), ts.rate(); got != expected {
		t.Errorf("Unexpected value. Got %v, want %v", got, expected)
	}

}

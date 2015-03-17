package mounttable

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"v.io/v23/naming"
	"v.io/v23/security"
	vdltime "v.io/v23/vdlroot/time"
)

var now = time.Now()

type fakeTime struct {
	theTime time.Time
}

func (ft *fakeTime) now() time.Time {
	return ft.theTime
}
func (ft *fakeTime) advance(d time.Duration) {
	ft.theTime = ft.theTime.Add(d)
}
func NewFakeTimeClock() *fakeTime {
	return &fakeTime{theTime: now}
}

func TestServerList(t *testing.T) {
	eps := []string{
		"endpoint:adfasdf@@who",
		"endpoint:sdfgsdfg@@x/",
		"endpoint:sdfgsdfg@@y",
		"endpoint:dfgsfdg@@",
	}

	// Test adding entries.
	ft := NewFakeTimeClock()
	setServerListClock(ft)
	sl := newServerList()
	for i, ep := range eps {
		bp := security.BlessingPattern(fmt.Sprintf("ep%d", i))
		sl.add(ep, []security.BlessingPattern{bp}, time.Duration(5*i)*time.Second)
	}
	if sl.len() != len(eps) {
		t.Fatalf("got %d, want %d", sl.len(), len(eps))
	}

	// Test timing out entries.
	ft.advance(6 * time.Second)
	if sl.removeExpired() != len(eps)-2 {
		t.Fatalf("got %d, want %d", sl.len(), len(eps)-2)
	}

	// Test removing entries.
	sl.remove(eps[2])
	if sl.len() != len(eps)-3 {
		t.Fatalf("got %d, want %d", sl.len(), len(eps)-3)
	}

	// Test copyToSlice.
	if got, want := sl.copyToSlice(), []naming.MountedServer{
		{
			Server:           "endpoint:dfgsfdg@@",
			BlessingPatterns: []string{"ep3"},
			Deadline:         vdltime.Deadline{now.Add(15 * time.Second)},
		},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
}

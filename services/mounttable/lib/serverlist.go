package mounttable

import (
	"container/list"
	"sync"
	"time"

	"veyron2/services/mounttable"
)

type serverListClock interface {
	now() time.Time
}

type realTime bool

func (t realTime) now() time.Time {
	return time.Now()
}

// TODO(caprita): Replace this with the timekeeper library.
var slc = serverListClock(realTime(true))

// server maintains the state of a single server.  Unless expires is refreshed before the
// time is reached, the entry will be removed.
type server struct {
	expires time.Time
	oa      string // object address of server
}

// serverList represents an ordered list of servers.
type serverList struct {
	sync.Mutex
	l *list.List // contains entries of type *server
}

// NewServerList creates a synchronized list of servers.
func NewServerList() *serverList {
	return &serverList{l: list.New()}
}

// set up an alternate clock.
func setServerListClock(x serverListClock) {
	slc = x
}

func (sl *serverList) len() int {
	sl.Lock()
	defer sl.Unlock()
	return sl.l.Len()
}

func (sl *serverList) Front() *server {
	sl.Lock()
	defer sl.Unlock()
	return sl.l.Front().Value.(*server)
}

// add to the front of the list if not already in the list, otherwise,
// update the expiration time and move to the front of the list.  That
// way the most recently refreshed is always first.
func (sl *serverList) add(oa string, ttl time.Duration) {
	expires := slc.now().Add(ttl)
	sl.Lock()
	defer sl.Unlock()
	for e := sl.l.Front(); e != nil; e = e.Next() {
		s := e.Value.(*server)
		if s.oa == oa {
			s.expires = expires
			sl.l.MoveToFront(e)
			return
		}
	}
	s := &server{
		oa:      oa,
		expires: expires,
	}
	sl.l.PushFront(s) // innocent until proven guilty
}

// remove an element from the list.  Return the number of elements remaining.
func (sl *serverList) remove(oa string) int {
	sl.Lock()
	defer sl.Unlock()
	for e := sl.l.Front(); e != nil; e = e.Next() {
		s := e.Value.(*server)
		if s.oa == oa {
			sl.l.Remove(e)
			break
		}
	}
	return sl.l.Len()
}

// removeExpired removes any expired servers.
func (sl *serverList) removeExpired() int {
	sl.Lock()
	defer sl.Unlock()

	now := slc.now()
	var next *list.Element
	for e := sl.l.Front(); e != nil; e = next {
		s := e.Value.(*server)
		next = e.Next()
		if now.After(s.expires) {
			sl.l.Remove(e)
		}
	}
	return sl.l.Len()
}

// copyToSlice returns the contents of the list as a slice of MountedServer.
func (sl *serverList) copyToSlice() []mounttable.MountedServer {
	sl.Lock()
	defer sl.Unlock()
	var slice []mounttable.MountedServer
	now := slc.now()
	for e := sl.l.Front(); e != nil; e = e.Next() {
		s := e.Value.(*server)
		ttl := uint32(s.expires.Sub(now).Seconds())
		ms := mounttable.MountedServer{Server: s.oa, TTL: ttl}
		slice = append(slice, ms)
	}
	return slice
}

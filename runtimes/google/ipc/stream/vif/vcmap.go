package vif

import (
	"sort"
	"sync"

	"v.io/veyron/veyron/runtimes/google/ipc/stream/id"
	"v.io/veyron/veyron/runtimes/google/ipc/stream/vc"
	"v.io/veyron/veyron/runtimes/google/lib/pcqueue"
)

// vcMap implements a thread-safe map of vc.VC objects (vcInfo) keyed by their VCI.
type vcMap struct {
	mu     sync.Mutex
	m      map[id.VC]vcInfo
	frozen bool
}

// vcInfo represents per-VC information maintained by a VIF.
type vcInfo struct {
	VC *vc.VC
	// Queues used to dispatch work to per-VC goroutines.
	// RQ is where vif.readLoop can dispatch work to.
	// WQ is where vif.writeLoop can dispatch work to.
	RQ, WQ *pcqueue.T
}

func newVCMap() *vcMap { return &vcMap{m: make(map[id.VC]vcInfo)} }

func (m *vcMap) Insert(c *vc.VC) (inserted bool, rq, wq *pcqueue.T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.frozen {
		return false, nil, nil
	}
	if _, exists := m.m[c.VCI()]; exists {
		return false, nil, nil
	}
	info := vcInfo{VC: c, RQ: pcqueue.New(100), WQ: pcqueue.New(100)}
	m.m[c.VCI()] = info
	return true, info.RQ, info.WQ
}

func (m *vcMap) Find(vci id.VC) (vc *vc.VC, rq, wq *pcqueue.T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := m.m[vci]
	return info.VC, info.RQ, info.WQ
}

func (m *vcMap) Delete(vci id.VC) {
	m.mu.Lock()
	if info, exists := m.m[vci]; exists {
		info.RQ.Close()
		info.WQ.Close()
		delete(m.m, vci)
	}
	m.mu.Unlock()
}

func (m *vcMap) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.m)
}

// Freeze causes all subsequent Inserts to fail.
// Returns a list of all the VCs that are in the map.
func (m *vcMap) Freeze() []vcInfo {
	m.mu.Lock()
	m.frozen = true
	l := make([]vcInfo, 0, len(m.m))
	for _, info := range m.m {
		l = append(l, info)
	}
	m.mu.Unlock()
	return l
}

type vcSlice []*vc.VC

func (s vcSlice) Len() int           { return len(s) }
func (s vcSlice) Less(i, j int) bool { return s[i].VCI() < s[j].VCI() }
func (s vcSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// List returns the list of all VCs currently in the map, sorted by VCI
func (m *vcMap) List() []*vc.VC {
	m.mu.Lock()
	l := make([]*vc.VC, 0, len(m.m))
	for _, info := range m.m {
		l = append(l, info.VC)
	}
	m.mu.Unlock()
	sort.Sort(vcSlice(l))
	return l
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vif

import (
	"sort"
	"sync"

	"v.io/x/ref/profiles/internal/lib/pcqueue"
	"v.io/x/ref/profiles/internal/rpc/stream/id"
	"v.io/x/ref/profiles/internal/rpc/stream/vc"
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
	info := vcInfo{
		VC: c,
		RQ: pcqueue.New(100),
		WQ: pcqueue.New(100),
	}
	m.m[c.VCI()] = info
	return true, info.RQ, info.WQ
}

func (m *vcMap) Find(vci id.VC) (vc *vc.VC, rq, wq *pcqueue.T) {
	m.mu.Lock()
	info := m.m[vci]
	m.mu.Unlock()
	return info.VC, info.RQ, info.WQ
}

// Delete deletes the given VC and returns true if the map is empty after deletion.
func (m *vcMap) Delete(vci id.VC) bool {
	m.mu.Lock()
	if info, exists := m.m[vci]; exists {
		info.RQ.Close()
		info.WQ.Close()
		delete(m.m, vci)
	}
	empty := len(m.m) == 0
	m.mu.Unlock()
	return empty
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

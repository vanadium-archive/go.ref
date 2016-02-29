// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"sync"

	"github.com/pborman/uuid"

	idiscovery "v.io/x/ref/lib/discovery"
)

type scanner struct {
	mu   sync.Mutex
	uuid uuid.UUID
	ch   chan *idiscovery.AdInfo
	done bool
}

func (s *scanner) handleLost(id uuid.UUID, oldAd *bleAd) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}

	adinfo := &idiscovery.AdInfo{Lost: true}
	copy(adinfo.Ad.Id[:], oldAd.attrs[IdUuid])
	s.ch <- adinfo
	return nil
}

func (s *scanner) handleUpdate(id uuid.UUID, oldAd *bleAd, newAd *bleAd) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}

	adinfo, err := newAd.toAdInfo()
	if err != nil {
		return err
	}
	s.ch <- adinfo
	return nil
}

func (s *scanner) stop() {
	s.mu.Lock()
	s.done = true
	close(s.ch)
	s.mu.Unlock()
}

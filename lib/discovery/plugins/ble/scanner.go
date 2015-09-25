// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"log"
	"sync"

	"v.io/x/ref/lib/discovery"

	"github.com/pborman/uuid"
)

type scanner struct {
	mu   sync.Mutex
	uuid uuid.UUID
	ch   chan *discovery.Advertisement
	done bool
}

func (s *scanner) handleChange(id uuid.UUID, oldAdv *bleAdv, newAdv *bleAdv) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}
	if oldAdv != nil {
		a, err := oldAdv.toDiscoveryAdvertisement()
		if err != nil {
			log.Println("failed to convert advertisement:", err)
		}
		a.Lost = true
		s.ch <- a
	}

	if newAdv != nil {
		a, err := newAdv.toDiscoveryAdvertisement()
		if err != nil {
			return err
		}
		s.ch <- a
	}
	return nil
}

func (s *scanner) stop() {
	s.mu.Lock()
	s.done = true
	close(s.ch)
	s.mu.Unlock()
}

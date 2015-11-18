// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ble

import (
	"sync"

	"github.com/pborman/uuid"

	vdiscovery "v.io/v23/discovery"

	"v.io/x/ref/lib/discovery"
)

type scanner struct {
	mu   sync.Mutex
	uuid uuid.UUID
	ch   chan *discovery.Advertisement
	done bool
}

func (s *scanner) handleLost(id uuid.UUID, oldAdv *bleAdv) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}

	s.ch <- &discovery.Advertisement{
		Service: vdiscovery.Service{
			InstanceId: string(oldAdv.attrs[InstanceIdUuid]),
		},
		Lost: true,
	}
	return nil
}

func (s *scanner) handleUpdate(id uuid.UUID, oldAdv *bleAdv, newAdv *bleAdv) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}

	a, err := newAdv.toDiscoveryAdvertisement()
	if err != nil {
		return err
	}
	s.ch <- a
	return nil
}

func (s *scanner) stop() {
	s.mu.Lock()
	s.done = true
	close(s.ch)
	s.mu.Unlock()
}

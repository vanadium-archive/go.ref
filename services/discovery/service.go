// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"sync"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/discovery"
	"v.io/v23/rpc"
	"v.io/v23/security"
	sdiscovery "v.io/v23/services/discovery"
	"v.io/v23/verror"
)

const pkgPath = "v.io/x/ref/services/discovery"

const (
	maxActiveHandles = int(^uint16(0)) // 65535.
)

var (
	errTooManyServices = verror.Register(pkgPath+".errTooManyServices", verror.NoRetry, "{1:}{2:} too many registered services")
)

type impl struct {
	ctx *context.T
	d   discovery.T

	mu         sync.Mutex
	handles    map[sdiscovery.ServiceHandle]func() // GUARDED_BY(mu)
	lastHandle sdiscovery.ServiceHandle            // GUARDED_BY(mu)
}

func (s *impl) RegisterService(ctx *context.T, call rpc.ServerCall, service discovery.Service, visibility []security.BlessingPattern) (sdiscovery.ServiceHandle, error) {
	ctx, cancel := context.WithCancel(s.ctx)
	if err := s.d.Advertise(ctx, service, visibility); err != nil {
		cancel()
		return 0, err
	}

	s.mu.Lock()
	if len(s.handles) >= maxActiveHandles {
		s.mu.Unlock()
		cancel()
		return 0, verror.New(errTooManyServices, ctx)
	}
	handle := s.lastHandle + 1
	for {
		if handle == 0 { // Avoid zero handle.
			handle++
		}
		if _, exist := s.handles[handle]; !exist {
			break
		}
	}
	s.handles[handle] = cancel
	s.lastHandle = handle
	s.mu.Unlock()
	return handle, nil
}

func (s *impl) UnregisterService(ctx *context.T, call rpc.ServerCall, handle sdiscovery.ServiceHandle) error {
	s.mu.Lock()
	cancel := s.handles[handle]
	delete(s.handles, handle)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (s *impl) Scan(ctx *context.T, call sdiscovery.ScannerScanServerCall, query string) error {
	updateCh, err := s.d.Scan(ctx, query)
	if err != nil {
		return err
	}

	stream := call.SendStream()
	for update := range updateCh {
		if err = stream.Send(update); err != nil {
			return err
		}
	}
	return nil
}

// NewDiscoveryService returns a new Discovery service implementation.
func NewDiscoveryService(ctx *context.T) sdiscovery.DiscoveryServerMethods {
	return &impl{
		ctx:     ctx,
		d:       v23.GetDiscovery(ctx),
		handles: make(map[sdiscovery.ServiceHandle]func()),
	}
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"github.com/pborman/uuid"

	"v.io/v23/context"
	"v.io/v23/discovery"
)

// Scan implements discovery.Scanner.
func (ds *ds) Scan(ctx *context.T, query string) (<-chan discovery.Update, error) {
	// TODO(jhann): Implement a simple query processor.
	var serviceUuid uuid.UUID
	if len(query) > 0 {
		serviceUuid = NewServiceUUID(query)
	}
	// TODO(jhahn): Revisit the buffer size.
	scanCh := make(chan *Advertisement, 10)
	ctx, cancel := context.WithCancel(ctx)
	for _, plugin := range ds.plugins {
		err := plugin.Scan(ctx, serviceUuid, scanCh)
		if err != nil {
			cancel()
			return nil, err
		}
	}
	// TODO(jhahn): Revisit the buffer size.
	updateCh := make(chan discovery.Update, 10)
	go doScan(ctx, scanCh, updateCh)
	return updateCh, nil
}

func doScan(ctx *context.T, scanCh <-chan *Advertisement, updateCh chan<- discovery.Update) {
	defer close(updateCh)
	for {
		select {
		case ad := <-scanCh:
			// TODO(jhahn): Merge scanData based on InstanceUuid.
			// TODO(jhahn): Handle "Lost" case.
			update := discovery.UpdateFound{
				Value: discovery.Found{Service: ad.Service},
			}
			updateCh <- update
		case <-ctx.Done():
			return
		}
	}
}

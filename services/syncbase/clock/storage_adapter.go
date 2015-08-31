// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock

import (
	"v.io/v23/context"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/store"
)

var _ StorageAdapter = (*storageAdapterImpl)(nil)

func NewStorageAdapter(st store.Store) StorageAdapter {
	return &storageAdapterImpl{st}
}

type storageAdapterImpl struct {
	st store.Store
}

func (sa *storageAdapterImpl) GetClockData(ctx *context.T, data *ClockData) error {
	return util.Get(ctx, sa.st, clockDataKey(), data)
}

func (sa *storageAdapterImpl) SetClockData(ctx *context.T, data *ClockData) error {
	return util.Put(ctx, sa.st, clockDataKey(), data)
}

func clockDataKey() string {
	return util.ClockPrefix
}

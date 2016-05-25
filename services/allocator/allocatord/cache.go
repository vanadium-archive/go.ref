// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"sync"

	"github.com/golang/groupcache/lru"

	"v.io/v23/context"
	"v.io/v23/verror"
)

var (
	ownerCache      *lru.Cache
	ownerCacheMutex sync.Mutex
)

func checkOwner(ctx *context.T, email, kName string) error {
	ownerCacheMutex.Lock()
	if ownerCache == nil {
		ownerCache = lru.New(maxInstancesFlag)
	} else if v, ok := ownerCache.Get(kName); ok {
		ownerCacheMutex.Unlock()
		if email != v {
			return verror.New(verror.ErrNoExistOrNoAccess, nil)
		}
		return nil
	}
	ownerCacheMutex.Unlock()

	if err := isOwnerOfInstance(ctx, email, kName); err != nil {
		return err
	}
	ownerCacheMutex.Lock()
	ownerCache.Add(kName, email)
	ownerCacheMutex.Unlock()
	return nil
}

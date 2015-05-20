// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// When applications update objects in the local Store, the sync
// watcher thread learns about them asynchronously via the "watch"
// stream of object mutations. In turn, this sync watcher thread
// updates the DAG and ILog records to track the object change
// histories.

// watchStore processes updates obtained by watching the store.
func (s *syncService) watchStore() {
	defer s.pending.Done()
}

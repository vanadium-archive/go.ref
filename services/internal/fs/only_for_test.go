// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fs

import (
	"v.io/v23/naming"
)

// TP is a convenience function. It prepends the transactionNamePrefix
// to the given path.
func TP(path string) string {
	return naming.Join(transactionNamePrefix, path)
}

func (ms *Memstore) PersistedFile() string {
	return ms.persistedFile
}

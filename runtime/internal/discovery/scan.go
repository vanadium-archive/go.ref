// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package discovery

import (
	"v.io/v23/context"
	"v.io/v23/discovery"
)

// Scan implements discovery.Scanner.
func (ds *ds) Scan(ctx *context.T, query string) (<-chan discovery.Update, error) {
	return nil, nil
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpc

import (
	"v.io/v23/rpc"
)

func InternalServerResolveToEndpoint(s rpc.Server, name string) (string, error) {
	return s.(*server).resolveToEndpoint(name)
}

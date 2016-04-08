// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package control

import (
	"v.io/v23/context"
)

// InternalCtx returns the controller's internal context so that it may be used
// by tests.
// TODO(nlacasse): Once we have better idea of how syncbase clients will
// operate, we should consider getting rid of this.
func (c *Controller) InternalCtx() *context.T {
	return c.ctx
}

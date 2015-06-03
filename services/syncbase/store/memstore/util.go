// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memstore

import (
	"v.io/v23/verror"
)

func convertError(err error) error {
	return verror.Convert(verror.IDAction{}, nil, err)
}

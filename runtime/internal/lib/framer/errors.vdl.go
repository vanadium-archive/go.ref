// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated by the vanadium vdl tool.
// Source: errors.vdl

package framer

import (
	// VDL system imports
	"v.io/v23/context"
	"v.io/v23/i18n"
	"v.io/v23/verror"
)

var (
	ErrLargerThan3ByteUInt = verror.Register("v.io/x/ref/runtime/internal/lib/framer.LargerThan3ByteUInt", verror.NoRetry, "{1:}{2:} integer too large to represent in 3 bytes")
)

func init() {
	i18n.Cat().SetWithBase(i18n.LangID("en"), i18n.MsgID(ErrLargerThan3ByteUInt.ID), "{1:}{2:} integer too large to represent in 3 bytes")
}

// NewErrLargerThan3ByteUInt returns an error with the ErrLargerThan3ByteUInt ID.
func NewErrLargerThan3ByteUInt(ctx *context.T) error {
	return verror.New(ErrLargerThan3ByteUInt, ctx)
}

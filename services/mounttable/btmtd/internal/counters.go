// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"bytes"
	"encoding/binary"

	"google.golang.org/cloud/bigtable"

	"v.io/v23/context"
	"v.io/v23/verror"
)

func incrementCounter(ctx *context.T, bt *BigTable, name string, delta int64) (int64, error) {
	bctx, cancel := btctx(ctx)
	defer cancel()

	m := bigtable.NewReadModifyWrite()
	m.Increment(metadataFamily, "c", delta)
	row, err := bt.counterTbl.ApplyReadModifyWrite(bctx, name, m)
	if err != nil {
		return 0, err
	}
	return decodeCounterValue(ctx, row)
}

func decodeCounterValue(ctx *context.T, row bigtable.Row) (c int64, err error) {
	if len(row[metadataFamily]) != 1 {
		return 0, verror.NewErrInternal(ctx)
	}
	b := row[metadataFamily][0].Value
	err = binary.Read(bytes.NewReader(b), binary.BigEndian, &c)
	return
}

func incrementCreatorNodeCount(ctx *context.T, bt *BigTable, creator string, delta int64) error {
	_, err := incrementCounter(ctx, bt, "num-nodes-per-user:"+creator, delta)
	return err
}

func incrementCreatorServerCount(ctx *context.T, bt *BigTable, creator string, delta int64) error {
	_, err := incrementCounter(ctx, bt, "num-servers-per-user:"+creator, delta)
	return err
}

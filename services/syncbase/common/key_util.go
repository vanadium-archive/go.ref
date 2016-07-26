// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package common

import (
	"fmt"
	"strconv"
	"strings"

	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

// TODO(ivanpi): Switch helpers here and in v23 to verror and eliminate wrapping
// into verror.ErrInternal at callsites.

// JoinKeyParts builds keys for accessing data in the storage engine.
func JoinKeyParts(parts ...string) string {
	return strings.Join(parts, KeyPartSep)
}

// SplitKeyParts is the inverse of JoinKeyParts. Clients are generally
// encouraged to use SplitNKeyParts.
func SplitKeyParts(key string) []string {
	return strings.Split(key, KeyPartSep)
}

// SplitNKeyParts is to SplitKeyParts as strings.SplitN is to strings.Split.
func SplitNKeyParts(key string, n int) []string {
	return strings.SplitN(key, KeyPartSep, n)
}

// StripFirstKeyPartOrDie strips off the first part of the given key. Typically
// used to strip off the key prefixes defined in constants.go. Panics if the
// input string has fewer than two parts.
func StripFirstKeyPartOrDie(key string) string {
	parts := SplitNKeyParts(key, 2)
	if len(parts) < 2 {
		vlog.Fatalf("StripFirstKeyPartOrDie: invalid key %q", key)
	}
	return parts[1]
}

// FirstKeyPart returns the first part of 'key', typically a key prefix defined
// in constants.go.
func FirstKeyPart(key string) string {
	return SplitNKeyParts(key, 2)[0]
}

// EncodeIdKeyPart encodes an id for embedding in a store key. No escaping is
// necessary since valid ids are guaranteed not to contain reserved key bytes
// such as '\xfe'. Ids are encoded as "<blessing>\x00<name>\x00" to preserve
// the natural sort order (first by blessing, then by name) even when embedded
// in store keys.
func EncodeIdKeyPart(id wire.Id) string {
	return id.Blessing + IdPartSep + id.Name + IdPartSep
}

// DecodeIdKeyPart is the inverse of EncodeIdKeyPart.
func DecodeIdKeyPart(idKeyPart string) (wire.Id, error) {
	parts := strings.SplitN(idKeyPart, IdPartSep, 3)
	if len(parts) < 3 || parts[2] != "" {
		return wire.Id{}, fmt.Errorf("DecodeIdKeyPart: invalid id part %q", idKeyPart)
	}
	return wire.Id{Blessing: parts[0], Name: parts[1]}, nil
}

// IsRowKey returns true iff 'key' is a storage engine key for a row.
func IsRowKey(key string) bool {
	return FirstKeyPart(key) == RowPrefix
}

// ParseRowKey extracts collection and row parts from the given storage engine
// key for a data row. Returns an error if the given key is not a storage engine
// key for a data row.
func ParseRowKey(key string) (collection wire.Id, row string, err error) {
	parts := SplitNKeyParts(key, 3)
	pfx := parts[0]
	if len(parts) < 3 || pfx != RowPrefix {
		return wire.Id{}, "", fmt.Errorf("ParseRowKey: invalid key %q", key)
	}
	cxId, err := DecodeIdKeyPart(parts[1])
	if err != nil {
		return wire.Id{}, "", fmt.Errorf("ParseRowKey: invalid collection %q: %v", parts[1], err)
	}
	return cxId, parts[2], nil
}

// ParseCollectionPermsKey extracts the collection id from the given storage
// engine key for a collection perms entry. Returns an error if the given key
// is not a storage engine key for a collection perms entry.
func ParseCollectionPermsKey(key string) (collection wire.Id, err error) {
	// TODO(rdaoud,ivanpi): See hack in collection.go.
	parts := SplitNKeyParts(key, 3)
	pfx := parts[0]
	if len(parts) < 3 || pfx != CollectionPermsPrefix || parts[2] != "" {
		return wire.Id{}, fmt.Errorf("ParseCollectionPermsKey: invalid key %q", key)
	}
	cxId, err := DecodeIdKeyPart(parts[1])
	if err != nil {
		return wire.Id{}, fmt.Errorf("ParseCollectionPermsKey: invalid collection %q: %v", parts[1], err)
	}
	return cxId, nil
}

// ScanPrefixArgs returns args for sn.Scan() for the specified prefix.
func ScanPrefixArgs(stKeyPrefix, prefix string) ([]byte, []byte) {
	return ScanRangeArgs(stKeyPrefix, util.PrefixRangeStart(prefix), util.PrefixRangeLimit(prefix))
}

// ScanRangeArgs returns args for sn.Scan() for the specified range.
// If limit is "", all rows with keys >= start are included.
func ScanRangeArgs(stKeyPrefix, start, limit string) ([]byte, []byte) {
	fullStart, fullLimit := JoinKeyParts(stKeyPrefix, start), JoinKeyParts(stKeyPrefix, limit)
	if limit == "" {
		fullLimit = util.PrefixRangeLimit(fullLimit)
	}
	return []byte(fullStart), []byte(fullLimit)
}

type BatchType byte

const (
	BatchTypeSn BatchType = 's' // snapshot
	BatchTypeTx           = 't' // transaction
)

// JoinBatchHandle encodes batch type and id into a BatchHandle.
func JoinBatchHandle(batchType BatchType, batchId uint64) wire.BatchHandle {
	return wire.BatchHandle(fmt.Sprintf("%c%016x", rune(batchType), batchId))
}

// SplitBatchHandle is the inverse of JoinBatchHandle.
func SplitBatchHandle(bh wire.BatchHandle) (BatchType, uint64, error) {
	if len(bh) != 1+16 {
		return BatchTypeSn, 0, verror.New(verror.ErrBadArg, nil, bh, "invalid batch handle length")
	}
	batchTypeByte, batchIdStr := bh[0], string(bh[1:])
	batchType := BatchType(batchTypeByte)
	if batchType != BatchTypeSn && batchType != BatchTypeTx {
		return BatchTypeSn, 0, verror.New(verror.ErrBadArg, nil, bh, "invalid batch type")
	}
	batchId, err := strconv.ParseUint(batchIdStr, 16, 64)
	if err != nil {
		return BatchTypeSn, 0, verror.New(verror.ErrBadArg, nil, bh, err)
	}
	return batchType, batchId, nil
}

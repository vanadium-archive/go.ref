// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package server

import (
	"strings"

	"v.io/syncbase/v23/services/syncbase"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
)

type dispatcher struct {
	s *service
}

var _ rpc.Dispatcher = (*dispatcher)(nil)

func NewDispatcher(s *service) *dispatcher {
	return &dispatcher{s: s}
}

// TODO(sadovsky): Return a real authorizer in various places below.
func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	suffix = strings.TrimPrefix(suffix, "/")
	parts := strings.Split(suffix, "/")

	// Validate all key atoms up front, so that we can avoid doing so in all our
	// method implementations.
	for _, s := range parts {
		if !validKeyAtom(s) {
			// TODO(sadovsky): Is it okay to pass a nil context to verror?
			return nil, nil, syncbase.NewErrInvalidName(nil, suffix)
		}
	}

	if len(parts) == 0 {
		return syncbase.ServiceServer(d.s), nil, nil
	}

	universe := &universe{
		name: parts[0],
		s:    d.s,
	}
	if len(parts) == 1 {
		return syncbase.UniverseServer(universe), nil, nil
	}

	database := &database{
		name: parts[1],
		u:    universe,
	}
	if len(parts) == 2 {
		return syncbase.DatabaseServer(database), nil, nil
	}

	table := &table{
		name: parts[2],
		d:    database,
	}
	if len(parts) == 3 {
		return syncbase.TableServer(table), nil, nil
	}

	item := &item{
		encodedKey: parts[3],
		t:          table,
	}
	if len(parts) == 4 {
		return syncbase.ItemServer(item), nil, nil
	}

	// TODO(sadovsky): Is it okay to pass a nil context to verror?
	return nil, nil, verror.NewErrNoExist(nil)
}

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flow

import (
	"strings"

	"v.io/v23/context"
	"v.io/v23/verror"
)

var noWrapPackages = []string{
	"v.io/v23/verror",
	"v.io/v23/flow",
}

func MaybeWrapError(idAction verror.IDAction, ctx *context.T, err error) error {
	if !shouldWrap(err) {
		return err
	}
	return verror.New(idAction, ctx, err)
}

func shouldWrap(err error) bool {
	if !isVError(err) {
		return true
	}
	id := verror.ErrorID(err)
	for _, pkg := range noWrapPackages {
		if strings.HasPrefix(string(id), pkg) {
			return false
		}
	}
	return true
}

func isVError(err error) bool {
	if _, ok := err.(verror.E); ok {
		return true
	}
	if e, ok := err.(*verror.E); ok && e != nil {
		return true
	}
	return false
}

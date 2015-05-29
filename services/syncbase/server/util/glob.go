// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package util

import (
	"strings"

	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/glob"
)

// NOTE(nlacasse): Syncbase handles Glob requests by implementing
// GlobChildren__ at each level (service, app, database, table).
//
// Each GlobChildren__ method returns all the children of the name without
// filtering them in any way. Any filtering that needs to happen based on the
// glob pattern happens magically at a higher level. This results in a simple
// GlobChildren__ implementation, but often an inefficient one, since we must
// return every single child name, even though many will be filtered before
// being sent to the client.
//
// We should consider changing GlobChildren__ so that it takes the pattern that
// is being Globbed as an argument. Then, GlobChildren__ could be smarter about
// what it returns, and only return names that already match the pattern. This
// would prevent us from having to read all child names from the database every
// time we Glob.

// globPatternToPrefix takes "foo*" and returns "foo".
// It assumes the input pattern is a valid glob pattern, and returns
// verror.ErrBadArg if the input is not a valid prefix.
func globPatternToPrefix(pattern string) (string, error) {
	if pattern == "" {
		return "", verror.NewErrBadArg(nil)
	}
	if pattern[len(pattern)-1] != '*' {
		return "", verror.NewErrBadArg(nil)
	}
	res := pattern[:len(pattern)-1]
	// Disallow chars and char sequences that have special meaning in glob, since
	// our Glob() does not support these.
	if strings.ContainsAny(res, "/*?[") {
		return "", verror.NewErrBadArg(nil)
	}
	if strings.Contains(res, "...") {
		return "", verror.NewErrBadArg(nil)
	}
	return res, nil
}

// Takes ownership of sn.
// TODO(sadovsky): It sucks that Glob must be implemented differently from other
// streaming RPC handlers. I don't have much confidence that I've implemented
// both types of streaming correctly.
func Glob(ctx *context.T, call rpc.ServerCall, pattern string, sn store.Snapshot, stKeyPrefix string) (<-chan string, error) {
	// TODO(sadovsky): Support glob with non-prefix pattern.
	if _, err := glob.Parse(pattern); err != nil {
		sn.Close()
		return nil, verror.New(verror.ErrBadArg, ctx, err)
	}
	prefix, err := globPatternToPrefix(pattern)
	if err != nil {
		sn.Close()
		if verror.ErrorID(err) == verror.ErrBadArg.ID {
			return nil, verror.NewErrNotImplemented(ctx)
		}
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	it := sn.Scan(ScanPrefixArgs(stKeyPrefix, prefix))
	ch := make(chan string)
	go func() {
		defer sn.Close()
		defer close(ch)
		key := []byte{}
		for it.Advance() {
			key = it.Key(key)
			parts := SplitKeyParts(string(key))
			ch <- parts[len(parts)-1]
		}
		if err := it.Err(); err != nil {
			// TODO(nlacasse): We should do something with the error other than
			// just logging. Ideally we could send the error back to the
			// client, but the GlobChildren__ API doesn't really allow for that
			// since it uses a channel of strings rather than GlobResults.
			vlog.VI(1).Infof("Glob(%q) failed: %v", pattern, err)
		}
	}()
	return ch, nil
}

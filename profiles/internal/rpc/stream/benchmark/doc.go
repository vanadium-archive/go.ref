// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package benchmark implements some benchmarks for comparing the
// v.io/v23/x/ref/profiles/internal/rpc/stream implementation with raw TCP
// connections and/or pipes.
//
// Sample usage:
//	go test v.io/v23/x/ref/profiles/internal/rpc/stream/benchmark -bench=.
package benchmark

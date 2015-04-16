// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import "testing"

func Benchmark_dial_VIF_NoSecurity(b *testing.B) { benchmarkDialVIF(b, securityNone) }
func Benchmark_dial_VIF(b *testing.B)            { benchmarkDialVIF(b, securityDefault) }

// Note: We don't benchmark SecurityNone VC Dial for now since it doesn't wait ack
// from the server after sending "OpenVC".
func Benchmark_dial_VC(b *testing.B) { benchmarkDialVC(b, securityDefault) }

// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package id provides types for identifying VCs and Flows over them.
package id

// VC identifies a VC over a VIF.
type VC uint32

// Flow identifies a Flow over a VC.
type Flow uint32

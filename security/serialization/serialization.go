// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package serialization defines a general-purpose io.WriteCloser
// for writing data along with an appropriate signature that
// establishes integrity and authenticity of data, and an io.Reader
// for reading the data after verifying the signature.
package serialization

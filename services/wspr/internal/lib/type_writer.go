// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

// TypeWriter implements io.Writer but allows changing the underlying buffer.
// This is useful for merging discrete messages that are part of the same flow.
type TypeWriter struct {
	writer ClientWriter
}

func NewTypeWriter(w ClientWriter) *TypeWriter {
	return &TypeWriter{writer: w}
}

func (w *TypeWriter) Write(p []byte) (int, error) {
	err := w.writer.Send(ResponseTypeMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

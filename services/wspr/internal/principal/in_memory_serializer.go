// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package principal

import (
	"io"

	"v.io/x/ref/lib/security"
)

// inMemorySerializer implements SerializerReaderWriter. This Serializer should only
// be used in tests.
type inMemorySerializer struct {
	data      bufferCloser
	signature bufferCloser
	hasData   bool
}

var _ security.SerializerReaderWriter = (*inMemorySerializer)(nil)

func NewInMemorySerializer() *inMemorySerializer {
	return &inMemorySerializer{}
}

func (s *inMemorySerializer) Readers() (io.ReadCloser, io.ReadCloser, error) {
	if !s.hasData {
		return nil, nil, nil
	}
	return &s.data, &s.signature, nil
}

func (s *inMemorySerializer) Writers() (io.WriteCloser, io.WriteCloser, error) {
	s.hasData = true
	s.data.Reset()
	s.signature.Reset()
	return &s.data, &s.signature, nil
}

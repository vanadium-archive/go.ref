package principal

import (
	"bytes"
	"io"
)

// bufferCloser implements io.ReadWriteCloser.
type bufferCloser struct {
	bytes.Buffer
}

func (*bufferCloser) Close() error {
	return nil
}

// InMemorySerializer implements Serializer. This Serializer should only be
// used in tests.
// TODO(ataly, bjornick): Get rid of all uses of this Serializer from non-test
// code and use a file backed (or some persistent storage backed) Serializer there
// instead.
type InMemorySerializer struct {
	data      bufferCloser
	signature bufferCloser
	hasData   bool
}

func (s *InMemorySerializer) Readers() (io.Reader, io.Reader, error) {
	if !s.hasData {
		return nil, nil, nil
	}
	return &s.data, &s.signature, nil
}

func (s *InMemorySerializer) Writers() (io.WriteCloser, io.WriteCloser, error) {
	s.hasData = true
	s.data.Reset()
	s.signature.Reset()
	return &s.data, &s.signature, nil
}

package identity

import (
	"bytes"
	"io"
)

type bufferCloser struct {
	bytes.Buffer
}

func (*bufferCloser) Close() error {
	return nil
}

type InMemorySerializer struct {
	data      bufferCloser
	signature bufferCloser
	hasData   bool
}

func (s *InMemorySerializer) DataWriter() io.WriteCloser {
	s.hasData = true
	s.data.Reset()
	return &s.data
}

func (s *InMemorySerializer) SignatureWriter() io.WriteCloser {
	s.signature.Reset()
	return &s.signature
}

func (s *InMemorySerializer) DataReader() io.Reader {
	if s.hasData {
		return &s.data
	}
	return nil
}

func (s *InMemorySerializer) SignatureReader() io.Reader {
	if s.hasData {
		return &s.signature
	}
	return nil
}

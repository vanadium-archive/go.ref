package security

import (
	"io"
)

// SerializerReaderWriter is a factory for managing the readers and writers used for
// serialization and deserialization of signed data.
type SerializerReaderWriter interface {
	// Readers returns io.ReadCloser for reading serialized data and its
	// integrity signature.
	Readers() (data io.ReadCloser, signature io.ReadCloser, err error)
	// Writers returns io.WriteCloser for writing serialized data and its
	// integrity signature.
	Writers() (data io.WriteCloser, signature io.WriteCloser, err error)
}

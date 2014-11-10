package security

import (
	"io"
	"os"
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

// FileSerializer implements SerializerReaderWriter that persists state to files.
type FileSerializer struct {
	data      *os.File
	signature *os.File

	dataFilePath      string
	signatureFilePath string
}

// NewFileSerializer creates a FileSerializer with the given data and signature files.
func NewFileSerializer(dataFilePath, signatureFilePath string) (*FileSerializer, error) {
	data, err := os.Open(dataFilePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	signature, err := os.Open(signatureFilePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return &FileSerializer{
		data:              data,
		signature:         signature,
		dataFilePath:      dataFilePath,
		signatureFilePath: signatureFilePath,
	}, nil
}

func (fs *FileSerializer) Readers() (io.ReadCloser, io.ReadCloser, error) {
	if fs.data == nil || fs.signature == nil {
		return nil, nil, nil
	}
	return fs.data, fs.signature, nil
}

func (fs *FileSerializer) Writers() (io.WriteCloser, io.WriteCloser, error) {
	// Remove previous version of the files
	os.Remove(fs.dataFilePath)
	os.Remove(fs.signatureFilePath)
	var err error
	if fs.data, err = os.Create(fs.dataFilePath); err != nil {
		return nil, nil, err
	}
	if fs.signature, err = os.Create(fs.signatureFilePath); err != nil {
		return nil, nil, err
	}
	return fs.data, fs.signature, nil
}

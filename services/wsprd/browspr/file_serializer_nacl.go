package browspr

import (
	"io"
	"os"
	"runtime/ppapi"
)

// fileSerializer implements vsecurity.SerializerReaderWriter that persists state to
// files with the pepper API.
type fileSerializer struct {
	system    ppapi.FileSystem
	data      *ppapi.FileIO
	signature *ppapi.FileIO

	dataFile      string
	signatureFile string
}

func (fs *fileSerializer) Readers() (data io.ReadCloser, sig io.ReadCloser, err error) {
	if fs.data == nil || fs.signature == nil {
		return nil, nil, nil
	}
	return fs.data, fs.signature, nil
}

func (fs *fileSerializer) Writers() (data io.WriteCloser, sig io.WriteCloser, err error) {
	// Remove previous version of the files
	fs.system.Remove(fs.dataFile)
	fs.system.Remove(fs.signatureFile)
	if fs.data, err = fs.system.Create(fs.dataFile); err != nil {
		return nil, nil, err
	}
	if fs.signature, err = fs.system.Create(fs.signatureFile); err != nil {
		return nil, nil, err
	}
	return fs.data, fs.signature, nil
}

func fileNotExist(err error) bool {
	pe, ok := err.(*os.PathError)
	return ok && pe.Err.Error() == "file not found"
}

func NewFileSerializer(dataFile, signatureFile string, system ppapi.FileSystem) (*fileSerializer, error) {
	data, err := system.Open(dataFile)
	if err != nil && !fileNotExist(err) {
		return nil, err
	}
	signature, err := system.Open(signatureFile)
	if err != nil && !fileNotExist(err) {
		return nil, err
	}
	return &fileSerializer{
		system:        system,
		data:          data,
		signature:     signature,
		dataFile:      dataFile,
		signatureFile: signatureFile,
	}, nil
}

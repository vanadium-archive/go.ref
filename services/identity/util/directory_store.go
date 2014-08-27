package util

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

// DirectoryStore implements a key-value store on a filesystem where data for each key is stored in its own file.
// TODO(suharshs): When vstore is ready replace this with the veyron store.
type DirectoryStore struct {
	dir string
}

func (s DirectoryStore) Exists(key string) (bool, error) {
	_, err := os.Stat(s.pathName(key))
	return !os.IsNotExist(err), nil
}

func (s DirectoryStore) Put(key, value string) error {
	return ioutil.WriteFile(s.pathName(key), []byte(value), 0600)
}

func (s DirectoryStore) Get(key string) (string, error) {
	bytes, err := ioutil.ReadFile(s.pathName(key))
	return string(bytes), err
}

func (s DirectoryStore) pathName(key string) string {
	return filepath.Join(string(s.dir), key)
}

// NewDirectoryStore returns a key-value store that uses one file per key,
// and places all data in the provided directory.
func NewDirectoryStore(dir string) (*DirectoryStore, error) {
	if len(dir) == 0 {
		return nil, fmt.Errorf("must provide non-empty directory name")
	}
	// Make the directory if it doesn't already exist.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	return &DirectoryStore{dir}, nil
}

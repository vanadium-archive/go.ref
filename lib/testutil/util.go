package testutil

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	isecurity "veyron/runtimes/google/security"

	"veyron2/security"
)

// FormatLogLine will prepend the file and line number of the caller
// at the specificied depth (as per runtime.Caller) to the supplied
// format and args and return a formatted string. It is useful when
// implementing functions that factor out error handling and reporting
// in tests.
func FormatLogLine(depth int, format string, args ...interface{}) string {
	_, file, line, _ := runtime.Caller(depth)
	nargs := []interface{}{filepath.Base(file), line}
	nargs = append(nargs, args...)
	return fmt.Sprintf("%s:%d: "+format, nargs...)
}

// DepthToExternalCaller determines the number of stack frames to the first
// enclosing caller that is external to the package that this function is
// called from. Drectory name is used as a proxy for package name,
// that is, the directory component of the file return runtime.Caller is
// compared to that of the lowest level caller until a different one is
// encountered as the stack is walked upwards.
func DepthToExternalCaller() int {
	_, file, _, _ := runtime.Caller(1)
	cwd := filepath.Dir(file)
	for d := 2; d < 10; d++ {
		_, file, _, _ := runtime.Caller(d)
		if cwd != filepath.Dir(file) {
			return d
		}
	}
	return 1
}

// SaveIdentityToFile saves the provided identity in Base64VOM format
// to a randomly created temporary file, and returns the path to the file.
// This function is meant to be used for testing purposes only, it panics
// if there is an error. The caller must ensure that the created file
// is removed once it is no longer needed.
func SaveIdentityToFile(id security.PrivateID) string {
	f, err := ioutil.TempFile("", strconv.Itoa(rand.Int()))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	filePath := f.Name()

	if err := security.SaveIdentity(f, id); err != nil {
		os.Remove(filePath)
		panic(err)
	}
	return filePath
}

// NewBlessedIdentity creates a new identity and blesses it using the provided blesser
// under the provided name. This function is meant to be used for testing purposes only,
// it panics if there is an error.
func NewBlessedIdentity(blesser security.PrivateID, name string) security.PrivateID {
	id, err := isecurity.NewChainPrivateID("test")
	if err != nil {
		panic(err)
	}

	blessedID, err := blesser.Bless(id.PublicID(), name, 5*time.Minute, nil)
	if err != nil {
		panic(err)
	}
	derivedID, err := id.Derive(blessedID)
	if err != nil {
		panic(err)
	}
	return derivedID
}

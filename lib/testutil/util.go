package testutil

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	isecurity "veyron/runtimes/google/security"

	"veyron2/security"
)

var (
	random      []byte
	randomMutex sync.Mutex
)

func generateRandomBytes(size int) []byte {
	buffer := make([]byte, size)
	offset := 0
	for {
		bits := int64(Rand.Int63())
		for i := 0; i < 8; i++ {
			buffer[offset] = byte(bits & 0xff)
			size--
			if size == 0 {
				return buffer
			}
			offset++
			bits >>= 8
		}
	}
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

// NewBlessedIdentity creates a new identity and blesses it using the provided blesser
// under the provided name. This function is meant to be used for testing purposes only,
// it panics if there is an error.
func NewBlessedIdentity(blesser security.PrivateID, name string) security.PrivateID {
	id, err := isecurity.NewPrivateID("test")
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

// RandomBytes generates the given number of random bytes.
func RandomBytes(size int) []byte {
	buffer := make([]byte, size)
	randomMutex.Lock()
	defer randomMutex.Unlock()
	// Generate a 10MB of random bytes since that is a value commonly
	// used in the tests.
	if len(random) == 0 {
		random = generateRandomBytes(10 << 20)
	}
	if size > len(random) {
		extra := generateRandomBytes(size - len(random))
		random = append(random, extra...)
	}
	start := Rand.Intn(len(random) - size + 1)
	copy(buffer, random[start:start+size])
	return buffer
}

// SaveACLToFile saves the provided ACL in JSON format to a randomly created
// temporary file, and returns the path to the file. This function is meant
// to be used for testing purposes only, it panics if there is an error. The
// caller must ensure that the created file is removed once it is no longer needed.
func SaveACLToFile(acl security.ACL) string {
	f, err := ioutil.TempFile("", "saved_acl")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := security.SaveACL(f, acl); err != nil {
		defer os.Remove(f.Name())
		panic(err)
	}
	return f.Name()
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

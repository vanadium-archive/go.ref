package testutil

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
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

// FindUnusedPort finds an unused port and returns it. Of course, no guarantees
// are made that the port will actually be available by the time the caller
// gets around to binding to it. If no port can be found, (0, nil) is returned.
// If an error occurs while creating a socket, that error is returned and the
// other return value is 0.
func FindUnusedPort() (int, error) {
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 1000; i++ {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
		if err != nil {
			return 0, err
		}

		port := int(1024 + random.Int31n(64512))
		sa := &syscall.SockaddrInet4{Port: port}
		err = syscall.Bind(fd, sa)
		syscall.Close(fd)
		if err == nil {
			return port, nil
		}
	}

	return 0, nil
}

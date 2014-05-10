// +build darwin freebsd linux netbsd openbsd

package follow

import (
	"os"
	"reflect"
	"runtime"
	"syscall"
)

// unsafeClose closes a file in a thread-safe manner. Specifically, it closes
// the file descriptor and nils out the finalizer to avoid (*File).close,
// which modifies various fields of the file.
func unsafeClose(file *os.File) error {
	// Nil out the finalizer that calls (*File).close. The finalizer is
	// associated with a hidden inner struct named "file".
	// See os/file_unix.go in the Go source.
	handle := reflect.ValueOf(*file).FieldByName("file").Interface()
	runtime.SetFinalizer(handle, nil)
	// Close the file descriptor.
	fd := int(file.Fd())
	return syscall.Close(fd)
}

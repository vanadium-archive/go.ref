package platform

// #include <sys/utsname.h>
// #include <errno.h>
import "C"

import (
	"fmt"

	"v.io/v23"
)

// Platform returns the description of the Platform this process is running on.
// A default value for v23.Platform is provided even if an error is
// returned; nil is never returned for the first return result.
func Platform() (*v23.Platform, error) {
	var t C.struct_utsname
	if r, err := C.uname(&t); r != 0 {
		return &v23.Platform{}, fmt.Errorf("uname failed: errno %d", err)
	}
	d := &v23.Platform{
		Vendor:  "google",
		Model:   "generic",
		System:  C.GoString(&t.sysname[0]),
		Version: C.GoString(&t.version[0]),
		Release: C.GoString(&t.release[0]),
		Machine: C.GoString(&t.machine[0]),
		Node:    C.GoString(&t.nodename[0]),
	}
	return d, nil
}

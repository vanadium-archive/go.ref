package platform

// #include <sys/utsname.h>
// #include <errno.h>
import "C"

import (
	"fmt"

	"v.io/core/veyron2"
)

// Platform returns the description of the Platform this process is running on.
// A default value for veyron2.Platform is provided even if an error is
// returned; nil is never returned for the first return result.
func Platform() (*veyron2.Platform, error) {
	var t C.struct_utsname
	if r, err := C.uname(&t); r != 0 {
		return &veyron2.Platform{}, fmt.Errorf("uname failed: errno %d", err)
	}
	d := &veyron2.Platform{
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

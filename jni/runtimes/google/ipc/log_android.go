// +build android

package ipc

// #cgo LDFLAGS: -llog
// #include <android/log.h>
import "C"

import (
	"bytes"
	"log"
	"unsafe"
)

var ctag *C.char = C.CString("com.veyron.runtimes.google.ipc")

// androidWriter writes it's output using Android's Log() function.
type androidWriter struct {
	buf []byte
}

func (aw *androidWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	err = nil
	for nlidx := bytes.IndexByte(p, '\n'); nlidx != -1; nlidx = bytes.IndexByte(p, '\n') {
		aw.buf = append(aw.buf, p[:nlidx]...)
		p = p[nlidx+1:]
		aw.buf = append(aw.buf, 0)
		cstr := (*C.char)(unsafe.Pointer(&aw.buf[0]))
		C.__android_log_write(C.ANDROID_LOG_INFO, ctag, cstr)
		aw.buf = aw.buf[:0]
	}
	aw.buf = append(aw.buf, p...)
	return
}

func init() {
	log.SetOutput(&androidWriter{})
}

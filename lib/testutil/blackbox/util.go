package blackbox

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	waitSeconds = 100 * time.Second
)

// Prepend the file+lineno of the first caller from outside this package
func formatLogLine(format string, args ...interface{}) string {
	_, file, line, _ := runtime.Caller(1)
	cwd := filepath.Dir(file)
	for d := 2; d < 10; d++ {
		_, file, line, _ = runtime.Caller(d)
		if cwd != filepath.Dir(file) {
			break
		}
	}
	nargs := []interface{}{filepath.Base(file), line}
	nargs = append(nargs, args...)
	return fmt.Sprintf("%s:%d: "+format, nargs...)
}

// Wait for EOF on os.Stdin, used to signal to the Child that it should exit.
func WaitForEOFOnStdin() {
	var buf [1]byte
	for {
		_, err := os.Stdin.Read(buf[:])
		if err != nil {
			break
		}
	}
}

// ReadLineFromStdin reads a line from os.Stdin, blocking until one is available
// or until EOF is encoutnered.  The line is returned with the newline character
// chopped off.
func ReadLineFromStdin() string {
	if read, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return ""
	} else {
		return strings.TrimRight(read, "\n")
	}
}

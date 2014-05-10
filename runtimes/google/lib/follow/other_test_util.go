// +build !darwin,!freebsd,!linux,!netbsd,!openbsd

package follow

import (
	"os"
)

func unsafeClose(file *os.File) error {
	panic("not implemented")
}

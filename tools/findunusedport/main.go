package main

// findunusedport finds a random unused TCP port in the range 1k to 64k and prints it to standard out.

import (
	"fmt"
	"math/rand"
	"os"
	"syscall"

	"v.io/core/veyron2/vlog"
)

func main() {
	rand.Seed(int64(os.Getpid()))
	for i := 0; i < 1000; i++ {
		port := 1024 + rand.Int31n(64512)
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
		if err != nil {
			continue
		}
		sa := &syscall.SockaddrInet4{Port: int(port)}
		if err := syscall.Bind(fd, sa); err != nil {
			continue
		}
		syscall.Close(fd)
		fmt.Println(port)
		return
	}
	vlog.Fatal("can't find unused port")
}

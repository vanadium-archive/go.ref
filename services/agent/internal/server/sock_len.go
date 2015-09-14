package server

// #include <sys/un.h>
import "C"

func GetMaxSockPathLen() int {
	var t C.struct_sockaddr_un
	return len(t.sun_path)
}

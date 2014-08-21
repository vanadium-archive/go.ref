// +build android

package ipc

import (
	"veyron2/ipc"
)

func newServerCall(call ipc.ServerCall, mArgs *methodArgs) *serverCall {
	return &serverCall{
		stream:     newStream(call, mArgs),
		ServerCall: call,
	}
}

type serverCall struct {
	stream
	ipc.ServerCall
}

package server

import (
	"veyron/services/wsprd/lib"
	"veyron2/ipc"
)

// A simple struct that wraps a stream with the sender api.  It
// will write to the stream synchronously.  Any error will still
// be written to clientWriter.
type senderWrapper struct {
	stream ipc.Stream
}

func (s senderWrapper) Send(item interface{}, w lib.ClientWriter) {
	if err := s.stream.Send(item); err != nil {
		w.Error(err)
	}
}

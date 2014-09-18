// A client stream helper.

package client

import (
	"veyron.io/veyron/veyron/services/wsprd/lib"
	"veyron.io/veyron/veyron2/ipc"
)

// A message that will be passed to the writeLoop function that will
// eventually write the message out to the stream.
type streamMessage struct {
	// The data to put on the stream.
	data interface{}
	// The client writer that will be used to send errors.
	w lib.ClientWriter
}

// A stream that will eventually write messages to the underlying stream.
// It isn't initialized with a stream, but rather a chan that will eventually
// provide a stream, so that it can accept sends before the underlying stream
// has been set up.
type QueueingStream chan *streamMessage

// Creates and returns a queueing stream that will starting writing to the
// stream provided by the ready channel.  It is expected that ready will only
// provide a single stream.
// TODO(bjornick): allow for ready to pass an error if the stream had any issues
// setting up.
func StartQueueingStream(ready chan ipc.Stream) QueueingStream {
	s := make(QueueingStream, 100)
	go s.writeLoop(ready)
	return s
}

func (q QueueingStream) Send(item interface{}, w lib.ClientWriter) {
	// TODO(bjornick): Reject the message if the queue is too long.
	message := streamMessage{data: item, w: w}
	q <- &message
}

func (q QueueingStream) Close() error {
	close(q)
	return nil
}

func (q QueueingStream) writeLoop(ready chan ipc.Stream) {
	stream := <-ready
	for value, ok := <-q; ok; value, ok = <-q {
		if !ok {
			break
		}
		if err := stream.Send(value.data); err != nil {
			value.w.Error(err)
		}
	}

	// If the stream is on the client side, then also close the stream.
	if call, ok := stream.(ipc.Call); ok {
		call.CloseSend()
	}
}

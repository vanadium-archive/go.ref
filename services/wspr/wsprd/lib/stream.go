// The set of streaming helper objects for wspr.

package lib

import (
	"veyron2/ipc"
)

// An interface for an asynchronous sender.
type sender interface {
	// Similar to ipc.Stream.Send, expect that instead of
	// returning an error, w.sendError will be called.
	Send(item interface{}, w clientWriter)
}

// A message that will be passed to the writeLoop function that will
// eventually write the message out to the stream.
type streamMessage struct {
	// The data to put on the stream.
	data interface{}
	// The client writer that will be used to send errors.
	w clientWriter
}

// A stream that will eventually write messages to the underlying stream.
// It isn't initialized with a stream, but rather a chan that will eventually
// provide a stream, so that it can accept sends before the underlying stream
// has been set up.
type queueingStream chan *streamMessage

// Creates and returns a queueing stream that will starting writing to the
// stream provided by the ready channel.  It is expected that ready will only
// provide a single stream.
// TODO(bjornick): allow for ready to pass an error if the stream had any issues
// setting up.
func startQueueingStream(ready chan ipc.Stream) queueingStream {
	s := make(queueingStream, 100)
	go s.writeLoop(ready)
	return s
}

func (q queueingStream) Send(item interface{}, w clientWriter) {
	// TODO(bjornick): Reject the message if the queue is too long.
	message := streamMessage{data: item, w: w}
	q <- &message
}

func (q queueingStream) Close() error {
	close(q)
	return nil
}

func (q queueingStream) writeLoop(ready chan ipc.Stream) {
	stream := <-ready
	for value, ok := <-q; ok; value, ok = <-q {
		if !ok {
			break
		}
		if err := stream.Send(value.data); err != nil {
			value.w.sendError(err)
		}
	}

	// If the stream is on the client side, then also close the stream.
	if call, ok := stream.(ipc.ClientCall); ok {
		call.CloseSend()
	}
}

// A simple struct that wraps a stream with the sender api.  It
// will write to the stream synchronously.  Any error will still
// be written to clientWriter.
type senderWrapper struct {
	stream ipc.Stream
}

func (s senderWrapper) Send(item interface{}, w clientWriter) {
	if err := s.stream.Send(item); err != nil {
		w.sendError(err)
	}
}

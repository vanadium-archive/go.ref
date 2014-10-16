package app

import (
	"fmt"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/vom"
	"veyron.io/wspr/veyron/services/wsprd/lib"
)

type initConfig struct {
	stream ipc.Stream
	inType vom.Type
}

type message struct {
	data   string
	writer lib.ClientWriter
}

// oustandingStream provides a stream-like api with the added ability to
// queue up messages if the stream hasn't been initialized first.  send
// can be called before init has been called, but no data will be sent
// until init is called.
type outstandingStream struct {
	// The channel on which the stream and the type
	// of data on the stream is sent after the stream
	// has been constructed.
	initChan chan *initConfig
	// The queue of messages to write out.
	messages chan *message
	// done will be notified when the stream has been closed.
	done chan bool
}

func newStream() *outstandingStream {
	os := &outstandingStream{
		initChan: make(chan *initConfig, 1),
		// We allow queueing up to 100 messages before init is called.
		// TODO(bjornick): Deal with the case that the queue is full.
		messages: make(chan *message, 100),
		done:     make(chan bool),
	}
	go os.loop()
	return os
}

func (os *outstandingStream) send(data string, w lib.ClientWriter) {
	if os.messages != nil {
		os.messages <- &message{data, w}
	}
}

func (os *outstandingStream) end() {
	if os.messages != nil {
		close(os.messages)
	}
	os.messages = nil
}

// Waits until the stream has been closed and all the messages
// have been drained.
func (os *outstandingStream) waitUntilDone() {
	<-os.done
}

func (os *outstandingStream) loop() {
	config := <-os.initChan
	for msg := range os.messages {
		payload, err := vom.JSONToObject(msg.data, config.inType)
		if err != nil {
			msg.writer.Error(fmt.Errorf("error while converting json to InStreamType (%s): %v", msg.data, err))
			continue
		}
		if err := config.stream.Send(payload); err != nil {
			msg.writer.Error(fmt.Errorf("failed to send on stream: %v", err))
		}

	}
	close(os.done)
	// If this is a client rpc, we need to call CloseSend on it.
	if call, ok := config.stream.(ipc.Call); ok {
		call.CloseSend()
	}
}

func (os *outstandingStream) init(stream ipc.Stream, inType vom.Type) {
	os.initChan <- &initConfig{stream, inType}
}

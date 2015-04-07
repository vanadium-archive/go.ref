// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"fmt"

	"v.io/v23/rpc"
	"v.io/x/ref/services/wspr/internal/lib"
)

type initConfig struct {
	stream rpc.Stream
}

type Closer interface {
	CloseSend() error
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
	// true if the stream has been closed.
	closed bool
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
	if !os.closed {
		os.messages <- &message{data, w}
	}
}

func (os *outstandingStream) end() {
	if !os.closed {
		close(os.messages)
		os.closed = true
	}
}

// Waits until the stream has been closed and all the messages
// have been drained.
func (os *outstandingStream) waitUntilDone() {
	<-os.done
}

func (os *outstandingStream) loop() {
	config := <-os.initChan
	for msg := range os.messages {
		var item interface{}
		if err := lib.VomDecode(msg.data, &item); err != nil {
			msg.writer.Error(fmt.Errorf("failed to decode stream arg from %v: %v", msg.data, err))
			break
		}
		if err := config.stream.Send(item); err != nil {
			msg.writer.Error(fmt.Errorf("failed to send on stream: %v", err))
		}
	}
	close(os.done)
	// If this is a stream that has a CloseSend, we need to call it.
	if call, ok := config.stream.(Closer); ok {
		call.CloseSend()
	}
}

func (os *outstandingStream) init(stream rpc.Stream) {
	os.initChan <- &initConfig{stream}
}

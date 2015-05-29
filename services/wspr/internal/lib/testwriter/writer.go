// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package testwriter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"v.io/v23/verror"
	"v.io/x/ref/services/wspr/internal/lib"
)

type TestHarness interface {
	Errorf(fmt string, a ...interface{})
}

type Writer struct {
	sync.Mutex
	TypeStream []lib.Response
	Stream     []lib.Response // TODO Why not use channel?
	err        error
	// If this channel is set then a message will be sent
	// to this channel after recieving a call to FinishMessage()
	notifier chan bool
}

func (w *Writer) Send(responseType lib.ResponseType, msg interface{}) error {
	// We serialize and deserialize the reponse so that we can do deep equal with
	// messages that contain non-exported structs.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(lib.Response{Type: responseType, Message: msg}); err != nil {
		return err
	}

	var r lib.Response

	if err := json.NewDecoder(&buf).Decode(&r); err != nil {
		return err
	}

	w.Lock()
	defer w.Unlock()
	if responseType == lib.ResponseTypeMessage {
		w.TypeStream = append(w.TypeStream, r)
	} else {
		w.Stream = append(w.Stream, r)
	}
	if w.notifier != nil {
		w.notifier <- true
	}
	return nil

}

// ImmediatelyConsumeItem consumes an item on the stream without waiting.
func (w *Writer) ImmediatelyConsumeItem() (lib.Response, error) {
	w.Lock()
	defer w.Unlock()

	if len(w.Stream) < 1 {
		return lib.Response{}, fmt.Errorf("Expected an item on the stream, none found")
	}

	item := w.Stream[0]
	w.Stream = w.Stream[1:]

	return item, nil
}

func (w *Writer) Error(err error) {
	w.err = err
}

func (w *Writer) streamLength() int {
	w.Lock()
	defer w.Unlock()
	return len(w.Stream)
}

// Waits until there is at least n messages in w.Stream. Returns an error if we timeout
// waiting for the given number of messages.
func (w *Writer) WaitForMessage(n int) error {
	if w.streamLength() >= n {
		return nil
	}
	w.Lock()
	w.notifier = make(chan bool, 1)
	w.Unlock()
	for w.streamLength() < n {
		select {
		case <-w.notifier:
			continue
		case <-time.After(10 * time.Second):
			return fmt.Errorf("timed out")
		}
	}
	w.Lock()
	w.notifier = nil
	w.Unlock()
	return nil
}

func CheckResponses(w *Writer, wantStream []lib.Response, wantTypes []lib.Response, wantErr error) error {
	if got, want := w.Stream, wantStream; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("streams don't match: got %#v, want %#v", got, want)
	}
	if got, want := w.TypeStream, wantTypes; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("streams don't match: got %#v, want %#v", got, want)
	}
	if got, want := w.err, wantErr; verror.ErrorID(got) != verror.ErrorID(want) {
		return fmt.Errorf("unexpected error, got: %#v, expected: %#v", got, want)
	}
	return nil
}

package follow

import (
	"errors"
	"testing"
)

func TestSendEvent(t *testing.T) {
	event := errors.New("event")

	// Test that 1) a nil can be sent 2) an error can be sent after a nil.
	events := make(chan error, 1)
	stop := make(chan struct{})
	if !sendEvent(events, nil, stop) {
		t.Fatal("Expected that the event will be sent")
	}
	if recv := <-events; recv != nil {
		t.Fatalf("Expected to receive nil")
	}
	if !sendEvent(events, event, stop) {
		t.Fatal("Expected that the event will be sent")
	}
	if recv := <-events; recv != event {
		t.Fatalf("Expected to receive %v", event)
	}

	// Test that nils are coalesced.
	events = make(chan error, 1)
	stop = make(chan struct{})
	if !sendEvent(events, nil, stop) {
		t.Fatal("Expected that the event will be sent")
	}
	if !sendEvent(events, nil, stop) {
		t.Fatal("Expected that the event will be sent")
	}
	if recv := <-events; recv != nil {
		t.Fatalf("Expected to receive nil")
	}

	// Test that an error is not sent if stop is closed.
	events = make(chan error, 1)
	stop = make(chan struct{})
	close(stop)
	// The stop signal may not be handled immediately.
	for sendEvent(events, event, stop) {
	}

	// Test that a nil is not sent if stop is closed.
	events = make(chan error, 1)
	stop = make(chan struct{})
	close(stop)
	// The stop signal may not be handled immediately.
	for sendEvent(events, nil, stop) {
	}

	// Test that an error is not sent if stop is closed while send is blocked.
	events = make(chan error, 1)
	stop = make(chan struct{})
	if !sendEvent(events, nil, stop) {
		t.Fatal("Expected that the event will be sent")
	}
	go close(stop)
	// The stop signal may not be handled immediately.
	for sendEvent(events, event, stop) {
	}
}

package ipc_test

import (
	"io"
	"testing"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/ipc"
	verror "v.io/core/veyron2/verror2"
)

type simple struct {
	done <-chan struct{}
}

func (s *simple) Sleep(call ipc.ServerContext) error {
	select {
	case <-s.done:
	case <-time.After(time.Hour):
	}
	return nil
}

func (s *simple) Ping(call ipc.ServerContext) (string, error) {
	return "pong", nil
}

func (s *simple) Source(call ipc.ServerCall, start int) error {
	i := start
	backoff := 25 * time.Millisecond
	for {
		select {
		case <-s.done:
			return nil
		case <-time.After(backoff):
			call.Send(i)
			i++
		}
		backoff *= 2
	}
}

func (s *simple) Sink(call ipc.ServerCall) (int, error) {
	i := 0
	for {
		if err := call.Recv(&i); err != nil {
			return i, err
		}
	}
}

func (s *simple) Inc(call ipc.ServerCall, inc int) (int, error) {
	i := 0
	for {
		if err := call.Recv(&i); err != nil {
			if err == io.EOF {
				// TODO(cnicolaou): this should return a verror, i.e.
				// verror.Make(verror.EOF, call), but for now we
				// return an io.EOF
				return i, io.EOF
			}
			return i, err
		}
		call.Send(i + inc)
	}
}

func TestSimpleRPC(t *testing.T) {
	name, fn := initServer(t, r)
	defer fn()

	client := veyron2.GetClient(r.NewContext())
	call, err := client.StartCall(r.NewContext(), name, "Ping", nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	response := ""
	var verr error
	err = call.Finish(&response, &verr)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if got, want := response, "pong"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSimpleStreaming(t *testing.T) {
	name, fn := initServer(t, r)
	defer fn()

	ctx := r.NewContext()
	inc := 1
	call, err := veyron2.GetClient(r.NewContext()).StartCall(ctx, name, "Inc", []interface{}{inc})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := 10
	for i := 0; i <= want; i++ {
		if err := call.Send(i); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		got := -1
		if err = call.Recv(&got); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if want := i + inc; got != want {
			t.Fatalf("got %d, want %d")
		}
	}
	call.CloseSend()
	final := -1
	verr := call.Finish(&final, &err)
	if verr != nil {
		t.Fatalf("unexpected error: %s", verr)

	}
	if !verror.Is(err, verror.Unknown.ID) || err.Error() != `v.io/core/veyron2/verror.Unknown:   EOF` {
		t.Errorf("wrong error: %#v", err)
	}
	/* TODO(cnicolaou): use this when verror2/vom transition is done.
	if err != nil && !verror.Is(err, verror.EOF.ID) {
		t.Fatalf("unexpected error: %#v", err)
	}
	*/
	if got := final; got != want {
		t.Fatalf("got %d, want %d")
	}
}

// The set of streaming helper objects for wspr.

package stream

import (
	"veyron/services/wsprd/lib"
)

// An interface for an asynchronous sender.
type Sender interface {
	// Similar to ipc.Stream.Send, except that instead of
	// returning an error, w.sendError will be called.
	Send(item interface{}, w lib.ClientWriter)
}

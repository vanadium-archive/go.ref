package event

// Note: This is in a seperate package because it is shared by both the builder
// package and the compilerd package.  Both of those define a main(), so one
// cannot import the other.

// Typed representation of data sent to stdin/stdout from a command.  These
// will be json-encoded and sent to the client.
type Event struct {
	// File associated with the command.
	File string
	// The text sent to stdin/stderr.
	Message string
	// Stream that the message was sent to, either "stdout" or "stderr".
	Stream string
	// Unix time, the number of nanoseconds elapsed since January 1, 1970 UTC.
	Timestamp int64
}

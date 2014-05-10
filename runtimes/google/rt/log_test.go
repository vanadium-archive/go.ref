package rt_test

import (
	"veyron/runtimes/google/rt"
)

func ExampleApplication() {
	r := rt.R()
	// Uses a the same Logger as the internal runtime and libraries.
	l := r.Logger()
	l.VI(2).Info("hello from my app, mixed in with system logs\n")
	// Creates a seperate Logger from the internal runtime and libraries.
	l, _ = r.NewLogger("myapp")
	l.VI(2).Info("hello from my app in my own log file\n")
}

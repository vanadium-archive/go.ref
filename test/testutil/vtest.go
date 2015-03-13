package testutil

// CallAndRecover calls the function f and returns the result of recover().
// This minimizes the scope of the deferred recover, to ensure f is actually the
// function that paniced.
func CallAndRecover(f func()) (result interface{}) {
	defer func() {
		result = recover()
	}()
	f()
	return
}

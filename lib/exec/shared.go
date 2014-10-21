package exec

const (
	version1     = "1.0.0"
	readyStatus  = "ready::"
	failedStatus = "failed::"
	initStatus   = "init"

	// The exec package uses this environment variable to communicate
	// the version of the protocol being used between the parent and child.
	// It takes care to clear this variable from the child process'
	// environment as soon as it can, however, there may still be some
	// situations where an application may need to test for its presence
	// or ensure that it doesn't appear in a set of environment variables;
	// exposing the name of this variable is intended to support such
	// situations.
	VersionVariable = "VEYRON_EXEC_VERSION"
)

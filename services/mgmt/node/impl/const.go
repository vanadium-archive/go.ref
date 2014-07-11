package impl

const (
	// BinaryEnv is the name of the environment variable that holds the
	// object name used for obtaining the node manager binary.
	BinaryEnv = "VEYRON_NM_BINARY"
	// PreviousEnv is the name of the environment variable that holds
	// the path to the workspace that contains the previous version of
	// the node manager.
	PreviousEnv = "VEYRON_NM_PREVIOUS"
	// OriginEnv is the name of the environment variable that holds the
	// object name of the application repository that can be used to
	// retrieve the node manager application envelope.
	OriginEnv = "VEYRON_NM_ORIGIN"
	// RootEnv is the name of the environment variable that holds the
	// path to the directory in which node manager workspaces are
	// created.
	RootEnv = "VEYRON_NM_ROOT"
)

package config

const (
	// EnvelopeEnv is the name of the environment variable that holds the
	// serialized node manager application envelope.
	EnvelopeEnv = "VEYRON_NM_ENVELOPE"
	// PreviousEnv is the name of the environment variable that holds the
	// path to the previous version of the node manager.
	PreviousEnv = "VEYRON_NM_PREVIOUS"
	// OriginEnv is the name of the environment variable that holds the
	// object name of the application repository that can be used to
	// retrieve the node manager application envelope.
	OriginEnv = "VEYRON_NM_ORIGIN"
	// RootEnv is the name of the environment variable that holds the
	// path to the directory in which node manager workspaces are
	// created.
	RootEnv = "VEYRON_NM_ROOT"
	// CurrentLinkEnv is the name of the environment variable that holds
	// the path to the soft link that points to the current node manager.
	CurrentLinkEnv = "VEYRON_NM_CURRENT"
	// HelperEnv is the name of the environment variable that holds the path
	// to the suid helper used to start apps as specific system users.
	HelperEnv = "VEYRON_NM_HELPER"
)

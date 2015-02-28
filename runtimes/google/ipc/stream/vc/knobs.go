package vc

const (
	// Maximum size (in bytes) of application data to write out in a single message.
	MaxPayloadSizeBytes = 1 << 16 // 64KB

	// Number of bytes that a receiver is willing to buffer for a flow.
	DefaultBytesBufferedPerFlow = 1 << 20 // 1MB

	// Maximum number of bytes to steal from the shared pool of receive
	// buffers for the first write of a new Flow.
	MaxSharedBytes = 1 << 12 // 4KB

	// Number of Flow IDs reserved for possible future use.
	NumReservedFlows = 10

	// Number of VC IDs reserved for special use.
	NumReservedVCs = 10

	// Special Flow ID used for information specific to the VC
	// (and not any specific flow)
	SharedFlowID = 0
)

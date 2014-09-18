package signature

import (
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/wiretype"
)

func TestNewJSONServiceSignature(t *testing.T) {
	sigIn := ipc.ServiceSignature{
		Methods: map[string]ipc.MethodSignature{
			"FirstMethod": ipc.MethodSignature{
				InArgs: []ipc.MethodArgument{
					ipc.MethodArgument{
						Name: "FirstArg",
						Type: wiretype.TypeIDFloat64,
					},
					ipc.MethodArgument{
						Name: "SecondArg",
						Type: wiretype.TypeIDUintptr,
					},
				},
				OutArgs: []ipc.MethodArgument{
					ipc.MethodArgument{
						Name: "FirstOutArg",
						Type: wiretype.TypeIDFloat64,
					},
					ipc.MethodArgument{
						Name: "SecondOutArg",
						Type: anydataTypeID,
					},
					ipc.MethodArgument{
						Name: "ThirdOutArg",
						Type: wiretype.TypeIDInt32,
					},
					ipc.MethodArgument{
						Name: "err",
						Type: wiretype.TypeIDFirst,
					},
				},
				OutStream: wiretype.TypeIDString,
			},
		},
		TypeDefs: []vdlutil.Any{
			anydataType,
			errType,
		},
	}

	sigOut := NewJSONServiceSignature(sigIn)

	expectedSigOut := JSONServiceSignature{
		"firstMethod": JSONMethodSignature{
			InArgs:      []string{"FirstArg", "SecondArg"},
			NumOutArgs:  4,
			IsStreaming: true,
		},
	}

	if !reflect.DeepEqual(sigOut, expectedSigOut) {
		t.Errorf("Signature differed from expectation. got: %v but expected %v", sigOut, expectedSigOut)
	}
}

func TestServiceSignature(t *testing.T) {
	sigIn := JSONServiceSignature{
		"firstMethod": JSONMethodSignature{
			InArgs:      []string{"FirstArg", "SecondArg"},
			NumOutArgs:  2,
			IsStreaming: true,
		},
	}

	sigOut, err := sigIn.ServiceSignature()
	if err != nil {
		t.Fatal("error in service signature", err)
	}

	expectedSigOut := ipc.ServiceSignature{
		Methods: map[string]ipc.MethodSignature{
			"FirstMethod": ipc.MethodSignature{
				InArgs: []ipc.MethodArgument{
					ipc.MethodArgument{
						Name: "FirstArg",
						Type: anydataTypeID,
					},
					ipc.MethodArgument{
						Name: "SecondArg",
						Type: anydataTypeID,
					},
				},
				OutArgs: []ipc.MethodArgument{
					ipc.MethodArgument{
						Type: anydataTypeID,
					},
					ipc.MethodArgument{
						Name: "err",
						Type: errTypeID,
					},
				},
				InStream:  anydataTypeID,
				OutStream: anydataTypeID,
			},
		},
		TypeDefs: []vdlutil.Any{
			anydataType,
			errType,
		},
	}

	if !reflect.DeepEqual(sigOut, expectedSigOut) {
		t.Error("Signature differed from expectation. got: %v but expected %v", sigOut, expectedSigOut)
	}
}

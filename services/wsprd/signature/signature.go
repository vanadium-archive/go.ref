package signature

import (
	"veyron.io/veyron/veyron/services/wsprd/lib"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/vdl/vdlutil"
	"veyron.io/veyron/veyron2/wiretype"
)

var (
	anydataType = wiretype.NamedPrimitiveType{
		Name: "veyron.io/veyron/veyron2/vdlutil.AnyData",
		Type: wiretype.TypeIDInterface,
	}
	errType = wiretype.NamedPrimitiveType{
		Name: "error",
		Type: wiretype.TypeIDInterface,
	}
	anydataTypeID = wiretype.TypeIDFirst
	errTypeID     = wiretype.TypeIDFirst
)

// JSONServiceSignature represents the information about a service signature that is used by JSON.
type JSONServiceSignature map[string]JSONMethodSignature

// JSONMethodSignature represents the information about a method signature that is used by JSON.
type JSONMethodSignature struct {
	InArgs      []string // InArgs is a list of argument names.
	NumOutArgs  int
	IsStreaming bool
}

// NewJSONServiceSignature converts an ipc service signature to the format used by JSON.
func NewJSONServiceSignature(sig ipc.ServiceSignature) JSONServiceSignature {
	jsig := JSONServiceSignature{}

	for name, methSig := range sig.Methods {
		jmethSig := JSONMethodSignature{
			InArgs:      make([]string, len(methSig.InArgs)),
			NumOutArgs:  len(methSig.OutArgs),
			IsStreaming: methSig.InStream != wiretype.TypeIDInvalid || methSig.OutStream != wiretype.TypeIDInvalid,
		}

		for i, inarg := range methSig.InArgs {
			jmethSig.InArgs[i] = inarg.Name
		}

		jsig[lib.LowercaseFirstCharacter(name)] = jmethSig
	}

	return jsig
}

// ServiceSignature converts a JSONServiceSignature to an ipc service signature.
func (jss JSONServiceSignature) ServiceSignature() (ipc.ServiceSignature, error) {
	ss := ipc.ServiceSignature{
		Methods: make(map[string]ipc.MethodSignature),
	}

	for name, sig := range jss {
		ms := ipc.MethodSignature{}

		ms.InArgs = make([]ipc.MethodArgument, len(sig.InArgs))
		for i, argName := range sig.InArgs {
			ms.InArgs[i] = ipc.MethodArgument{
				Name: argName,
				Type: anydataTypeID,
			}
		}

		ms.OutArgs = make([]ipc.MethodArgument, sig.NumOutArgs)
		for i := 0; i < sig.NumOutArgs-1; i++ {
			ms.OutArgs[i] = ipc.MethodArgument{
				Type: anydataTypeID,
			}
		}
		ms.OutArgs[sig.NumOutArgs-1] = ipc.MethodArgument{
			Name: "err",
			Type: errTypeID,
		}

		if sig.IsStreaming {
			ms.InStream = anydataTypeID
			ms.OutStream = anydataTypeID
		}

		ss.Methods[lib.UppercaseFirstCharacter(name)] = ms
	}

	ss.TypeDefs = []vdlutil.Any{anydataType, errType}

	return ss, nil
}

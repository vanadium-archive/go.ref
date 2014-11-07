package debug

import (
	"strings"

	"veyron.io/veyron/veyron2/ipc"
)

type signatureInvoker struct {
	suffix string
}

// NewSignatureInvoker is the invoker factory.
func NewSignatureInvoker(suffix string) ipc.Invoker {
	return ipc.ReflectInvoker(&signatureInvoker{suffix})
}

// TODO(rthellend): This is a temporary hack until https://code.google.com/p/envyor/issues/detail?id=285 is resolved.
func (s signatureInvoker) Signature(ipc.ServerCall) (ipc.ServiceSignature, error) {
	debugStub := DebugServer(nil)
	fullSig, _ := debugStub.Signature(nil)

	var show []string
	if strings.HasPrefix(s.suffix, "logs") {
		show = []string{"ReadLog", "Size", "Glob"}
	} else if strings.HasPrefix(s.suffix, "pprof") {
		show = []string{"Cmdline", "CPUProfile", "Profile", "Profiles", "Symbol"}
	} else if strings.HasPrefix(s.suffix, "stats") {
		show = []string{"Value", "Glob", "WatchGlob"}
	} else {
		show = []string{"Glob"}
	}

	sig := ipc.ServiceSignature{TypeDefs: fullSig.TypeDefs, Methods: make(map[string]ipc.MethodSignature)}
	for _, m := range show {
		sig.Methods[m] = fullSig.Methods[m]
	}
	return sig, nil
}

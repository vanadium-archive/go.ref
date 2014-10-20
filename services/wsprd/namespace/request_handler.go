package namespace

import (
	"encoding/json"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/verror2"

	"veyron.io/wspr/veyron/services/wsprd/lib"
)

// request struct represents a request to call a method on the namespace client
type request struct {
	Method namespaceMethod
	Args   json.RawMessage
	Roots  []string
}

type namespaceMethod int

// enumerates the methods available to be called on the namespace client
const (
	methodGlob namespaceMethod = 0
)

// globArgs defines the args for the glob method
type globArgs struct {
	Pattern string
}

// handleRequest uses the namespace client to respond to namespace specific requests such as glob
func HandleRequest(ctx context.T, rt veyron2.Runtime, data string, w lib.ClientWriter) {
	// Decode the request
	var req request
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	// Create a namespace and set roots if provided
	var ns = rt.Namespace()
	if len(req.Roots) > 0 {
		ns.SetRoots(req.Roots...)
	}

	switch req.Method {
	case methodGlob:
		glob(ctx, ns, w, req.Args)
	default:
		w.Error(verror2.Make(verror2.NoExist, ctx, req.Method))
	}
}

func glob(ctx context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args globArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	// Call glob on the namespace client instance
	ch, err := ns.Glob(ctx, args.Pattern)

	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	// Send the results as streams
	for name := range ch {
		if err := w.Send(lib.ResponseStream, name); err != nil {
			w.Error(verror2.Make(verror2.Internal, ctx, name))
			return
		}
	}

	if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseStreamClose"))
	}
}

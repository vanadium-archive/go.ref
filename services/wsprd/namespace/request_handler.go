package namespace

import (
	"encoding/json"
	"time"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/verror2"

	"v.io/wspr/veyron/services/wsprd/lib"
)

// Function to format endpoints.  Used by browspr to swap 'tcp' for 'ws'.
var EpFormatter func(veyron2.Runtime, []string) ([]string, error) = nil

// request struct represents a request to call a method on the runtime's namespace client
type request struct {
	Method namespaceMethod
	Args   json.RawMessage
}

type namespaceMethod int

// enumerates the methods available to be called on the runtime's namespace client
const (
	methodGlob            namespaceMethod = 0
	methodMount                           = 1
	methodUnmount                         = 2
	methodResolve                         = 3
	methodResolveToMt                     = 4
	methodFlushCacheEntry                 = 5
	methodDisableCache                    = 6
	methodRoots                           = 7
	methodSetRoots                        = 8
)

// globArgs defines the args for the glob method
type globArgs struct {
	Pattern string
}

// mountArgs defines the args for the mount method
type mountArgs struct {
	Name         string
	Server       string
	Ttl          time.Duration
	replaceMount bool
}

// unmountArgs defines the args for the unmount method
type unmountArgs struct {
	Name   string
	Server string
}

// resolveArgs defines the args for the resolve method
type resolveArgs struct {
	Name string
}

// resolveToMtArgs defines the args for the resolveToMt method
type resolveToMtArgs struct {
	Name string
}

// flushCacheEntryArgs defines the args for the flushCacheEntry method
type flushCacheEntryArgs struct {
	Name string
}

// disableCacheArgs defines the args for the disableCache method
type disableCacheArgs struct {
	Disable bool
}

// setRootsArgs defines the args for the setRoots method
type setRootsArgs struct {
	Roots []string
}

// handleRequest uses the namespace client to respond to namespace specific requests such as glob
func HandleRequest(ctx *context.T, rt veyron2.Runtime, data string, w lib.ClientWriter) {
	// Decode the request
	var req request
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	// Get the runtime's Namespace client
	var ns = rt.Namespace()

	switch req.Method {
	case methodGlob:
		glob(ctx, ns, w, req.Args)
	case methodMount:
		mount(ctx, ns, w, req.Args)
	case methodUnmount:
		unmount(ctx, ns, w, req.Args)
	case methodResolve:
		resolve(ctx, ns, w, req.Args)
	case methodResolveToMt:
		resolveToMt(ctx, ns, w, req.Args)
	case methodFlushCacheEntry:
		flushCacheEntry(ctx, ns, w, req.Args)
	case methodDisableCache:
		disableCache(ctx, ns, w, req.Args)
	case methodRoots:
		roots(ctx, ns, w)
	case methodSetRoots:
		setRoots(ctx, ns, rt, w, req.Args)
	default:
		w.Error(verror2.Make(verror2.NoExist, ctx, req.Method))
	}
}

func convertToVDLEntry(value naming.MountEntry) naming.VDLMountEntry {
	result := naming.VDLMountEntry{
		Name: value.Name,
		MT:   value.ServesMountTable(),
	}
	for _, s := range value.Servers {
		result.Servers = append(result.Servers,
			naming.VDLMountedServer{
				Server: s.Server,
				TTL:    uint32(s.Expires.Sub(time.Now())),
			})
	}
	return result
}

func glob(ctx *context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args globArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	// Call Glob on the namespace client instance
	ch, err := ns.Glob(ctx, args.Pattern)

	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	for mp := range ch {
		// send results that have error through the error stream.
		// TODO(aghassemi) we want mp to be part of the error's ParamsList, but there is no good way right now in verror2 to do this.
		if mp.Error != nil {
			err := verror2.Convert(verror2.Unknown, ctx, mp.Error)
			w.Error(err)
			continue
		}

		val, err := lib.VomEncode(convertToVDLEntry(mp))
		if err != nil {
			w.Error(verror2.Make(verror2.Internal, ctx, err))
			return
		}

		if err := w.Send(lib.ResponseStream, val); err != nil {
			w.Error(verror2.Make(verror2.Internal, ctx, mp))
			return
		}
	}

	if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseStreamClose"))
	}
}

func mount(ctx *context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args mountArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	rmOpt := naming.ReplaceMountOpt(args.replaceMount)
	err := ns.Mount(ctx, args.Name, args.Server, args.Ttl, rmOpt)

	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	if err := w.Send(lib.ResponseFinal, nil); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

func unmount(ctx *context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args unmountArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	err := ns.Unmount(ctx, args.Name, args.Server)

	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	if err := w.Send(lib.ResponseFinal, nil); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

func resolve(ctx *context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args resolveArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	addresses, err := ns.Resolve(ctx, args.Name)

	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	if err := w.Send(lib.ResponseFinal, addresses); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

func resolveToMt(ctx *context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args resolveToMtArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	addresses, err := ns.ResolveToMountTable(ctx, args.Name)

	if err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	if err := w.Send(lib.ResponseFinal, addresses); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

func flushCacheEntry(ctx *context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args flushCacheEntryArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	flushed := ns.FlushCacheEntry(args.Name)

	if err := w.Send(lib.ResponseFinal, flushed); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

func disableCache(ctx *context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args disableCacheArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	disableCacheCtl := naming.DisableCache(args.Disable)
	_ = ns.CacheCtl(disableCacheCtl)

	if err := w.Send(lib.ResponseFinal, nil); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

func roots(ctx *context.T, ns naming.Namespace, w lib.ClientWriter) {
	roots := ns.Roots()

	if err := w.Send(lib.ResponseFinal, roots); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

func setRoots(ctx *context.T, ns naming.Namespace, rt veyron2.Runtime, w lib.ClientWriter, rawArgs json.RawMessage) {
	var args setRootsArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	var formattedRoots []string
	var err error
	if EpFormatter != nil {
		formattedRoots, err = EpFormatter(rt, args.Roots)
		if err != nil {
			w.Error(verror2.Convert(verror2.Internal, ctx, err))
		}
	} else {
		formattedRoots = args.Roots
	}

	if err := ns.SetRoots(formattedRoots...); err != nil {
		w.Error(verror2.Convert(verror2.Internal, ctx, err))
		return
	}

	if err := w.Send(lib.ResponseFinal, nil); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

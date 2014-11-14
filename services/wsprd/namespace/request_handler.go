package namespace

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"time"

	"veyron.io/veyron/veyron2"
	"veyron.io/veyron/veyron2/context"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/verror2"
	"veyron.io/veyron/veyron2/vom2"

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
	methodGlob            namespaceMethod = 0
	methodMount                           = 1
	methodUnmount                         = 2
	methodResolve                         = 3
	methodResolveToMt                     = 4
	methodFlushCacheEntry                 = 5
	methodDisableCache                    = 6
	methodRoots                           = 7
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
	default:
		w.Error(verror2.Make(verror2.NoExist, ctx, req.Method))
	}
}

func encodeVom2(value interface{}) (string, error) {
	var buf bytes.Buffer
	encoder, err := vom2.NewBinaryEncoder(&buf)
	if err != nil {
		return "", err
	}

	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf.Bytes()), nil

}

func convertToVDLEntry(value naming.MountEntry) naming.VDLMountEntry {
	result := naming.VDLMountEntry{
		Name: value.Name,
		MT:   false,
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

func glob(ctx context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
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

	for name := range ch {
		val, err := encodeVom2(convertToVDLEntry(name))
		if err != nil {
			w.Error(verror2.Make(verror2.Internal, ctx, err))
			return
		}
		if err := w.Send(lib.ResponseStream, val); err != nil {
			w.Error(verror2.Make(verror2.Internal, ctx, name))
			return
		}
	}

	if err := w.Send(lib.ResponseStreamClose, nil); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseStreamClose"))
	}
}

func mount(ctx context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
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

func unmount(ctx context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
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

func resolve(ctx context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
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

func resolveToMt(ctx context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
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

func flushCacheEntry(ctx context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
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

func disableCache(ctx context.T, ns naming.Namespace, w lib.ClientWriter, rawArgs json.RawMessage) {
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

func roots(ctx context.T, ns naming.Namespace, w lib.ClientWriter) {
	roots := ns.Roots()

	if err := w.Send(lib.ResponseFinal, roots); err != nil {
		w.Error(verror2.Make(verror2.Internal, ctx, "ResponseFinal"))
	}
}

package namespace

import (
	inaming "v.io/core/veyron/runtimes/google/naming"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/verror"
)

type startStatus struct {
	index int
	err   error
	call  ipc.Call
}

func tryStartCall(ctx *context.T, client ipc.Client, target, method string, args []interface{}, c chan startStatus, index int) {
	call, err := client.StartCall(ctx, target, method, args, options.NoResolve{})
	c <- startStatus{index: index, err: err, call: call}
}

// parallelStartCall returns the first succeeding StartCall.
func (ns *namespace) parallelStartCall(ctx *context.T, client ipc.Client, servers []string, method string, args []interface{}) (ipc.Call, error) {
	if len(servers) == 0 {
		return nil, verror.New(verror.ErrNoExist, ctx, "no servers to resolve query")
	}

	// StartCall to each of the servers.
	c := make(chan startStatus, len(servers))
	cancelFuncs := make([]context.CancelFunc, len(servers))
	for index, server := range servers {
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		cancelFuncs[index] = cancel
		go tryStartCall(callCtx, client, server, method, args, c, index)
	}

	// First positive response wins.  Cancel the rest.  The cancellation
	// will prevent any RPCs from starting or progressing.  We do not close
	// the channel since some go routines may still be in flight and want to
	// write status to it.  The channel will be garbage collected when all
	// references to it disappear.
	var final startStatus
	for range servers {
		final = <-c
		if final.err == nil {
			cancelFuncs[final.index] = nil
			break
		}
	}
	// Cancel the rest.
	for _, cancel := range cancelFuncs {
		if cancel != nil {
			cancel()
		}
	}
	return final.call, final.err
}

type status struct {
	id  string
	err error
}

// nameToRID converts a name to a routing ID string. If a routing ID can't be obtained,
// it just returns the name.
func nameToRID(name string) string {
	address, _ := naming.SplitAddressName(name)
	if ep, err := inaming.NewEndpoint(address); err == nil {
		return ep.RID.String()
	}
	return name
}

// collectStati collects n status messages from channel c and returns an error if, for
// any id, there is no successful reply.
func collectStati(c chan status, n int) error {
	// Make a map indexed by the routing id (or address if routing id not found) of
	// each mount table.  A mount table may be reachable via multiple addresses but
	// each address should have the same routing id.  We should only return an error
	// if any of the ids had no successful mounts.
	statusByID := make(map[string]error)
	// Get the status of each request.
	for i := 0; i < n; i++ {
		s := <-c
		if _, ok := statusByID[s.id]; !ok || s.err == nil {
			statusByID[s.id] = s.err
		}
	}
	// Return any error.
	for _, s := range statusByID {
		if s != nil {
			return s
		}
	}
	return nil
}

// dispatch executes f in parallel for each mount table implementing mTName.
func (ns *namespace) dispatch(ctx *context.T, mTName string, f func(*context.T, string, string) status, opts ...naming.ResolveOpt) error {
	// Resolve to all the mount tables implementing name.
	me, err := ns.ResolveToMountTable(ctx, mTName, opts...)
	if err != nil {
		return err
	}
	mts := me.Names()
	// Apply f to each of the returned mount tables.
	c := make(chan status, len(mts))
	for _, mt := range mts {
		go func(mt string) {
			c <- f(ctx, mt, nameToRID(mt))
		}(mt)
	}
	finalerr := collectStati(c, len(mts))
	// Forget any previous cached information about these names.
	ns.resolutionCache.forget(mts)
	return finalerr
}

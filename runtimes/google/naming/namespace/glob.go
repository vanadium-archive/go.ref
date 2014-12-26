package namespace

import (
	"container/list"
	"io"
	"strings"

	"v.io/core/veyron/lib/glob"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
	"v.io/core/veyron2/options"
	verror "v.io/core/veyron2/verror2"
	"v.io/core/veyron2/vlog"
)

const mountTableGlobReplyStreamLength = 100

type queuedEntry struct {
	me    *naming.MountEntry
	depth int // number of mount tables traversed recursively
}

// globAtServer performs a Glob at a single server and adds any results to the list.  Paramters are:
//   server    the server to perform the glob at.  This may include multiple names for different
//             instances of the same server.
//   pelems    the pattern to match relative to the mounted subtree.
//   l         the list to add results to.
//   recursive true to continue below the matched pattern
func (ns *namespace) globAtServer(ctx context.T, qe *queuedEntry, pattern *glob.Glob, l *list.List) error {
	server := qe.me
	client := veyron2.RuntimeFromContext(ctx).Client()
	pstr := pattern.String()
	vlog.VI(2).Infof("globAtServer(%v, %v)", *server, pstr)

	var lastErr error
	// Trying each instance till we get one that works.
	for _, s := range server.Servers {
		// If the pattern is finished (so we're only querying about the root on the
		// remote server) and the server is not another MT, then we needn't send the
		// query on since we know the server will not supply a new address for the
		// current name.
		if pattern.Finished() {
			if !server.ServesMountTable() {
				return nil
			}
		}

		// If this is restricted recursive and not a mount table, don't
		// descend into it.
		if pattern.Restricted() && !server.ServesMountTable() && pattern.Len() == 0 {
			return nil
		}

		// Don't further resolve s.Server.
		callCtx, _ := ctx.WithTimeout(callTimeout)
		call, err := client.StartCall(callCtx, s.Server, ipc.GlobMethod, []interface{}{pstr}, options.NoResolve(true))
		if err != nil {
			lastErr = err
			continue // try another instance
		}

		// At this point we're commited to a server since it answered tha call.
		for {
			var e naming.VDLMountEntry
			err := call.Recv(&e)
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			// Prefix the results with the path of the mount point.
			e.Name = naming.Join(server.Name, e.Name)

			// Convert to the ever so slightly different name.MountTable version of a MountEntry
			// and add it to the list.
			x := &queuedEntry{
				me: &naming.MountEntry{
					Name:    e.Name,
					Servers: convertServers(e.Servers),
				},
				depth: qe.depth,
			}
			x.me.SetServesMountTable(e.MT)
			// x.depth is the number of severs we've walked through since we've gone
			// recursive (i.e. with pattern length of 0).
			if pattern.Len() == 0 {
				if x.depth++; x.depth > ns.maxRecursiveGlobDepth {
					continue
				}
			}
			l.PushBack(x)
		}

		var globerr error
		if err := call.Finish(&globerr); err != nil {
			return err
		}
		return globerr
	}

	return lastErr
}

// Glob implements naming.MountTable.Glob.
func (ns *namespace) Glob(ctx context.T, pattern string) (chan naming.MountEntry, error) {
	defer vlog.LogCall()()
	e, patternWasRooted := ns.rootMountEntry(pattern)
	if len(e.Servers) == 0 {
		return nil, verror.Make(naming.ErrNoMountTable, ctx)
	}

	// If pattern was already rooted, make sure we tack that root
	// onto all returned names.  Otherwise, just return the relative
	// name.
	var prefix string
	if patternWasRooted {
		prefix = e.Servers[0].Server
	}
	g, err := glob.Parse(e.Name)
	if err != nil {
		return nil, err
	}
	e.Name = ""
	reply := make(chan naming.MountEntry, 100)
	go ns.globLoop(ctx, e, prefix, g, reply)
	return reply, nil
}

// depth returns the directory depth of a given name.
func depth(name string) int {
	name = strings.Trim(naming.Clean(name), "/")
	if name == "" {
		return 0
	}
	return strings.Count(name, "/") + 1
}

func (ns *namespace) globLoop(ctx context.T, e *naming.MountEntry, prefix string, pattern *glob.Glob, reply chan naming.MountEntry) {
	defer close(reply)

	// As we encounter new mount tables while traversing the Glob, we add them to the list 'l'.  The loop below
	// traverses this list removing a mount table each time and calling globAtServer to perform a glob at that
	// server.  globAtServer will send on 'reply' any terminal entries that match the glob and add any new mount
	// tables to be traversed to the list 'l'.
	l := list.New()
	l.PushBack(&queuedEntry{me: e})
	atRoot := true

	// Perform a breadth first search of the name graph.
	for le := l.Front(); le != nil; le = l.Front() {
		l.Remove(le)
		e := le.Value.(*queuedEntry)

		// Get the pattern elements below the current path.
		suffix := pattern.Split(depth(e.me.Name))

		// Perform a glob at the server.
		err := ns.globAtServer(ctx, e, suffix, l)

		// We want to output this entry if:
		// 1. There was a real error, we return whatever name gave us the error.
		if err != nil && !notAnMT(err) {
			x := *e.me
			x.Name = naming.Join(prefix, x.Name)
			x.Error = err
			reply <- x
		}

		// 2. The current name fullfills the pattern.
		if suffix.Len() == 0 && !atRoot {
			x := *e.me
			x.Name = naming.Join(prefix, x.Name)
			reply <- x
		}
		atRoot = false
	}
}

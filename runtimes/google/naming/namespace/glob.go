package namespace

import (
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

// task is a sub-glob that has to be performed against a mount table.  Tasks are
// done in parrallel to speed up the glob.
type task struct {
	pattern *glob.Glob         // pattern to match
	me      *naming.MountEntry // server to match at
	depth   int                // number of mount tables traversed recursively
}

// globAtServer performs a Glob on the servers at a mount point.  It cycles through the set of
// servers until it finds one that replies.
func (ns *namespace) globAtServer(ctx *context.T, t *task, replies chan *task) {
	defer func() {
		if t.me.Error == nil {
			replies <- nil
		} else {
			replies <- t
		}
	}()
	client := veyron2.GetClient(ctx)
	pstr := t.pattern.String()
	vlog.VI(2).Infof("globAtServer(%v, %v)", *t.me, pstr)

	// We collect errors trying to connect to servers so that we have something to
	// return if we go through them all and noone answers.
	var lastErr error

	// Trying each instance till we get one that works.
	for _, s := range t.me.Servers {
		// Don't further resolve s.Server.
		callCtx, _ := context.WithTimeout(ctx, callTimeout)
		call, err := client.StartCall(callCtx, naming.JoinAddressName(s.Server, ""), ipc.GlobMethod, []interface{}{pstr}, options.NoResolve{})
		if err != nil {
			lastErr = err
			continue // try another instance
		}

		// At this point we're commited to a server since it answered the call.   Cycle
		// through all replies from that server.
		for {
			// If the mount table returns an error, we're done.  Send the task to the channel
			// including the error.  This terminates the task.
			var e naming.VDLMountEntry
			err := call.Recv(&e)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.me.Error = err
				return
			}

			// Convert to the ever so slightly different name.MountTable version of a MountEntry
			// and add it to the list.
			x := &task{
				me: &naming.MountEntry{
					Name:    naming.Join(t.me.Name, e.Name),
					Servers: convertServers(e.Servers),
				},
				depth: t.depth + 1,
			}
			x.me.SetServesMountTable(e.MT)

			// x.depth is the number of servers we've walked through since we've gone
			// recursive (i.e. with pattern length of 0).  Limit the depth of globs.
			// TODO(p): return an error?
			if t.pattern.Len() == 0 {
				if x.depth > ns.maxRecursiveGlobDepth {
					continue
				}
			}
			replies <- x
		}

		var globerr error
		if err := call.Finish(&globerr); err != nil {
			globerr = err
		}
		t.me.Error = globerr
		return
	}

	// Just soak up the last error (if any).
	t.me.Error = lastErr
}

// depth returns the directory depth of a given name.  It is used to pick off the unsatisfied part of the pattern.
func depth(name string) int {
	name = strings.Trim(naming.Clean(name), "/")
	if name == "" {
		return 0
	}
	return strings.Count(name, "/") + 1
}

// globLoop fires off a go routine for each server and read backs replies.
func (ns *namespace) globLoop(ctx *context.T, e *naming.MountEntry, prefix string, pattern *glob.Glob, reply chan naming.MountEntry) {
	defer close(reply)

	// Provide enough buffers to avoid too much switching between the readers and the writers.
	// This size is just a guess.
	replies := make(chan *task, 100)
	defer close(replies)

	// Push the first task into the channel to start the ball rolling.  This task has the
	// root of the search and the full pattern.  It will be the first task fired off in the for
	// loop that follows.
	replies <- &task{me: e, pattern: pattern}
	inFlight := 0

	// Perform a parallel search of the name graph.  Each task will send what it learns
	// on the replies channel.  If the reply is a mount point and the pattern is not completely
	// fulfilled, a new task will be fired off to handle it.
	for {
		select {
		case t := <-replies:
			// A nil reply represents a successfully terminated task.
			// If no tasks are running, return.
			if t == nil {
				if inFlight--; inFlight <= 0 {
					return
				}
				continue
			}

			// We want to output this entry if there was a real error other than
			// "not a mount table".
			// TODO(p): return errors on a different reply channel?
			//
			// An error reply is also a terminated task.
			// If no tasks are running, return.
			if t.me.Error != nil {
				if !notAnMT(t.me.Error) {
					t.me.Name = naming.Join(prefix, t.me.Name)
					reply <- *t.me
				}
				if inFlight--; inFlight <= 0 {
					return
				}
				continue
			}

			// Get the pattern elements below the current path.
			suffix := pattern.Split(depth(t.me.Name))

			// If we've satisfied the request and this isn't the root,
			// reply to the caller.
			if suffix.Len() == 0 && t.depth != 0 {
				x := *t.me
				x.Name = naming.Join(prefix, x.Name)
				reply <- x
			}

			// If the pattern is finished (so we're only querying about the root on the
			// remote server) and the server is not another MT, then we needn't send the
			// query on since we know the server will not supply a new address for the
			// current name.
			if suffix.Finished() {
				if !t.me.ServesMountTable() {
					continue
				}
			}

			// If this is restricted recursive and not a mount table, don't descend into it.
			if suffix.Restricted() && suffix.Len() == 0 && !t.me.ServesMountTable() {
				continue
			}

			// Perform a glob at the next server.
			inFlight++
			t.pattern = suffix
			go ns.globAtServer(ctx, t, replies)
		}
	}
}

// Glob implements naming.MountTable.Glob.
func (ns *namespace) Glob(ctx *context.T, pattern string) (chan naming.MountEntry, error) {
	defer vlog.LogCall()()

	// Root the pattern.  If we have no servers to query, give up.
	e, patternWasRooted := ns.rootMountEntry(pattern)
	if len(e.Servers) == 0 {
		return nil, verror.Make(naming.ErrNoMountTable, ctx)
	}

	// If the name doesn't parse, give up.
	g, err := glob.Parse(e.Name)
	if err != nil {
		return nil, err
	}

	// If pattern was already rooted, make sure we tack that root
	// onto all returned names.  Otherwise, just return the relative
	// name.
	var prefix string
	if patternWasRooted {
		prefix = e.Servers[0].Server
	}
	e.Name = ""
	reply := make(chan naming.MountEntry, 100)
	go ns.globLoop(ctx, e, prefix, g, reply)
	return reply, nil
}

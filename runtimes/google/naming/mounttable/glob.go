package mounttable

import (
	"container/list"
	"io"
	"strings"
	"time"

	"veyron/lib/glob"

	"veyron2/naming"
	"veyron2/vlog"
)

const mountTableGlobReplyStreamLength = 100

// globAtServer performs a Glob at a single server and adds any results to the list.  Paramters are:
//   server    the server to perform the glob at.  This may include multiple names for different
//             instances of the same server.
//   pelems    the pattern to match relative to the mounted subtree.
//   l         the list to add results to.
//   recursive true to continue below the matched pattern
// We return a bool foundRoot which indicates whether the empty name "" was found on a target server.
func (ns *namespace) globAtServer(server *naming.MountEntry, pattern *glob.Glob, l *list.List) (bool, error) {
	pstr := pattern.String()
	foundRoot := false
	vlog.VI(2).Infof("globAtServer(%v, %v)", *server, pstr)

	var lastErr error
	// Trying each instance till we get one that works.
	for _, s := range server.Servers {
		mtServers, err := ns.ResolveToMountTable(s.Server)
		if err != nil {
			lastErr = err
			continue
		}

		for _, s := range mtServers {
			client := ns.rt.Client()
			call, err := client.StartCall(ns.rt.TODOContext(), s, "Glob", []interface{}{pstr}, callTimeout)
			if err != nil {
				lastErr = err
				continue // try another instance
			}
			// At this point we have a server that can handle the RPC.
			for {
				var e mountEntry
				err := call.Recv(&e)
				if err == io.EOF {
					return foundRoot, nil
				}
				if err != nil {
					return foundRoot, err
				}
				if e.Name == "" {
					foundRoot = true
				}

				// Prefix the results with the path of the mount point.
				e.Name = naming.Join(server.Name, e.Name)

				// Convert to the ever so slightly different name.MountTable version of a MountEntry
				// and add it to the list.
				l.PushBack(&naming.MountEntry{
					Name:    e.Name,
					Servers: convertServers(e.Servers),
				})
			}

			if err := call.Finish(); err != nil {
				return foundRoot, err
			}
			return foundRoot, nil
		}
	}

	return foundRoot, lastErr
}

// Glob implements naming.MountTable.Glob.
func (ns *namespace) Glob(pattern string) (chan naming.MountEntry, error) {
	g, err := glob.Parse(pattern)
	if err != nil {
		return nil, err
	}

	// Add constant components of pattern to the servers' addresses and
	// to the prefix.
	var prefixElements []string
	prefixElements, g = g.SplitFixedPrefix()
	prefix := strings.Join(prefixElements, "/")

	// Start a thread to get the results and return the reply channel to the caller.
	servers := ns.rootName(prefix)
	if len(servers) == 0 {
		return nil, naming.ErrNoMountTable
	}
	reply := make(chan naming.MountEntry, 100)
	go ns.globLoop(servers, prefix, g, reply)
	return reply, nil
}

func convertStringsToServers(servers []string) (ret []naming.MountedServer) {
	for _, s := range servers {
		ret = append(ret, naming.MountedServer{Server: s})
	}
	return
}

// TODO(p):  I may just give up and assume that these two will always be the same.  For
// now this lets me make the RPC interface and the model's MountTable structs be arbitrarily
// different.
func convertServers(servers []mountedServer) []naming.MountedServer {
	var reply []naming.MountedServer
	for _, s := range servers {
		reply = append(reply, naming.MountedServer{Server: s.Server, TTL: time.Duration(s.TTL) * time.Second})
	}
	return reply
}

// depth returns the directory depth of a given name.
func depth(name string) int {
	name = strings.Trim(name, "/")
	if name == "" {
		return 0
	}
	return strings.Count(name, "/") - strings.Count(name, "//") + 1
}

func (ns *namespace) globLoop(servers []string, prefix string, pattern *glob.Glob, reply chan naming.MountEntry) {
	defer close(reply)

	l := list.New()
	l.PushBack(&naming.MountEntry{Name: "", Servers: convertStringsToServers(servers)})

	// Perform a breadth first search of the name graph.
	for le := l.Front(); le != nil; le = l.Front() {
		l.Remove(le)
		e := le.Value.(*naming.MountEntry)

		// Get the pattern elements below the current path.
		suffix := pattern.Split(depth(e.Name))

		// Perform a glob at the server.
		foundRoot, err := ns.globAtServer(e, suffix, l)

		// We want to output this entry if:
		// 1. There was a real error, we return whatever name gave us the error.
		if err != nil && !notAnMT(err) {
			x := *e
			x.Name = naming.Join(prefix, x.Name)
			x.Error = err
			reply <- x
		}

		// 2. The current name fullfills the pattern and further servers did not respond
		//    with "".  That is, we want to prefer foo/ over foo.
		if suffix.Len() == 0 && !foundRoot {
			x := *e
			x.Name = naming.Join(prefix, x.Name)
			reply <- x
		}
	}
}

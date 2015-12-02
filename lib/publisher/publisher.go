// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package publisher provides a type to publish names to a mounttable.
package publisher

// TODO(toddw): Add unittests.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

// Publisher manages the publishing of servers in mounttable.
type Publisher interface {
	// AddServer adds a new server to be mounted.
	AddServer(server string)
	// RemoveServer removes a server from the list of mounts.
	RemoveServer(server string)
	// AddName adds a new name for all servers to be mounted as.
	AddName(name string, ServesMountTable bool, IsLeaf bool)
	// RemoveName removes a name.
	RemoveName(name string)
	// Status returns a snapshot of the publisher's current state.
	Status() rpc.MountState
	// DebugString returns a string representation of the publisher
	// meant solely for debugging.
	DebugString() string
	// Stop causes the publishing to stop and initiates unmounting of the
	// mounted names.  Stop performs the unmounting asynchronously, and
	// WaitForStop should be used to wait until it is done.
	// Once Stop is called Add/RemoveServer and AddName become noops.
	Stop()
	// WaitForStop waits until all unmounting initiated by Stop is finished.
	WaitForStop()
}

// The publisher adds this much slack to each TTL.
const mountTTLSlack = 20 * time.Second

// publisher maintains the name->server associations in the mounttable.  It
// spawns its own goroutine that does the actual work; the publisher itself
// simply coordinates concurrent access by sending and receiving on the
// appropriate channels.
type publisher struct {
	cmdchan  chan interface{} // value is one of {server,name,debug}Cmd
	stopchan chan struct{}    // closed when no longer accepting commands.
	donechan chan struct{}    // closed when the publisher is done
	ctx      *context.T
}

type addServerCmd struct {
	server string // server to add
}

type removeServerCmd struct {
	server string // server to remove
}

type addNameCmd struct {
	name string // name to add
	mt   bool   // true if server serves a mount table
	leaf bool   // true if server is a leaf
}

type removeNameCmd struct {
	name string // name to remove
}

type debugCmd chan string // debug string is sent when the cmd is done

type statusCmd chan rpc.MountState // status info is sent when cmd is done

type stopCmd struct{} // sent to the runloop when we want it to exit.

// New returns a new publisher that updates mounts on ns every period.
func New(ctx *context.T, ns namespace.T, period time.Duration) Publisher {
	p := &publisher{
		cmdchan:  make(chan interface{}),
		stopchan: make(chan struct{}),
		donechan: make(chan struct{}),
		ctx:      ctx,
	}
	go runLoop(ctx, p.cmdchan, p.donechan, ns, period)
	return p
}

func (p *publisher) sendCmd(cmd interface{}) bool {
	select {
	case p.cmdchan <- cmd:
		return true
	case <-p.stopchan:
		return false
	case <-p.donechan:
		return false
	}
}

func (p *publisher) AddServer(server string) {
	p.sendCmd(addServerCmd{server})
}

func (p *publisher) RemoveServer(server string) {
	p.sendCmd(removeServerCmd{server})
}

func (p *publisher) AddName(name string, mt bool, leaf bool) {
	p.sendCmd(addNameCmd{name, mt, leaf})
}

func (p *publisher) RemoveName(name string) {
	p.sendCmd(removeNameCmd{name})
}

func (p *publisher) Status() rpc.MountState {
	status := make(statusCmd)
	if p.sendCmd(status) {
		return <-status
	}
	return rpc.MountState{}
}

func (p *publisher) DebugString() (dbg string) {
	debug := make(debugCmd)
	if p.sendCmd(debug) {
		dbg = <-debug
	} else {
		dbg = "stopped"
	}
	return
}

// Stop stops the publisher, which in practical terms means un-mounting
// everything and preventing any further publish operations.  The caller can
// be confident that no new names or servers will get published once Stop
// returns.  To wait for existing mounts to be cleaned up, use WaitForStop.
//
// Stopping the publisher is irreversible.
//
// Once the publisher is stopped, any further calls on its public methods
// (including Stop) are no-ops.
func (p *publisher) Stop() {
	p.sendCmd(stopCmd{})
	close(p.stopchan) // stop accepting new commands now.
}

func (p *publisher) WaitForStop() {
	<-p.donechan
}

func runLoop(ctx *context.T, cmdchan chan interface{}, donechan chan struct{}, ns namespace.T, period time.Duration) {
	ctx.VI(2).Info("rpc pub: start runLoop")
	state := newPubState(ctx, ns, period)
	for {
		select {
		case cmd := <-cmdchan:
			switch tcmd := cmd.(type) {
			case stopCmd:
				state.unmountAll()
				close(donechan)
				ctx.VI(2).Info("rpc pub: exit runLoop")
				return
			case addServerCmd:
				state.addServer(tcmd.server)
			case removeServerCmd:
				state.removeServer(tcmd.server)
			case addNameCmd:
				state.addName(tcmd.name, tcmd.mt, tcmd.leaf)
			case removeNameCmd:
				state.removeName(tcmd.name)
			case statusCmd:
				tcmd <- state.getStatus()
				close(tcmd)
			case debugCmd:
				tcmd <- state.debugString()
				close(tcmd)
			}
		case <-state.timeout():
			// Sync everything once every period, to refresh the ttls.
			state.sync()
		}
	}
}

type mountKey struct {
	name, server string
}

// pubState maintains the state for our periodic mounts.  It is not thread-safe;
// it's only used in the sequential publisher runLoop.
type pubState struct {
	ctx      *context.T
	ns       namespace.T
	period   time.Duration
	deadline time.Time           // deadline for the next sync call
	names    map[string]nameAttr // names that have been added
	servers  map[string]bool     // servers that have been added, true
	// map each (name,server) to its status.
	mounts map[mountKey]*rpc.MountStatus
}

type nameAttr struct {
	servesMT bool
	isLeaf   bool
}

func newPubState(ctx *context.T, ns namespace.T, period time.Duration) *pubState {
	return &pubState{
		ctx:      ctx,
		ns:       ns,
		period:   period,
		deadline: time.Now().Add(period),
		names:    make(map[string]nameAttr),
		servers:  make(map[string]bool),
		mounts:   make(map[mountKey]*rpc.MountStatus),
	}
}

func (ps *pubState) timeout() <-chan time.Time {
	return time.After(ps.deadline.Sub(time.Now()))
}

func (ps *pubState) addName(name string, mt bool, leaf bool) {
	// Each non-dup name that is added causes new mounts to be created for all
	// existing servers.
	if _, exists := ps.names[name]; exists {
		return
	}
	attr := nameAttr{mt, leaf}
	ps.names[name] = attr
	for server, _ := range ps.servers {
		status := new(rpc.MountStatus)
		ps.mounts[mountKey{name, server}] = status
		ps.mount(name, server, status, attr)
	}
}

func (ps *pubState) removeName(name string) {
	if _, exists := ps.names[name]; !exists {
		return
	}
	for server, _ := range ps.servers {
		if status, exists := ps.mounts[mountKey{name, server}]; exists {
			ps.unmount(name, server, status, true)
		}
	}
	delete(ps.names, name)
}

func (ps *pubState) addServer(server string) {
	// Each non-dup server that is added causes new mounts to be created for all
	// existing names.
	if _, exists := ps.servers[server]; !exists {
		ps.servers[server] = true
		for name, attr := range ps.names {
			status := new(rpc.MountStatus)
			ps.mounts[mountKey{name, server}] = status
			ps.mount(name, server, status, attr)
		}
	}
}

func (ps *pubState) removeServer(server string) {
	if _, exists := ps.servers[server]; !exists {
		return
	}
	delete(ps.servers, server)
	for name, _ := range ps.names {
		if status, exists := ps.mounts[mountKey{name, server}]; exists {
			ps.unmount(name, server, status, true)
		}
	}
}

func (ps *pubState) mount(name, server string, status *rpc.MountStatus, attr nameAttr) {
	// Always mount with ttl = period + slack, regardless of whether this is
	// triggered by a newly added server or name, or by sync.  The next call
	// to sync will occur within the next period, and refresh all mounts.
	ttl := ps.period + mountTTLSlack
	last := *status
	status.LastMount = time.Now()
	status.LastMountErr = ps.ns.Mount(ps.ctx, name, server, ttl, naming.ServesMountTable(attr.servesMT), naming.IsLeaf(attr.isLeaf))
	status.TTL = ttl
	// If the mount status changed, log it.
	if status.LastMountErr != nil {
		if verror.ErrorID(last.LastMountErr) != verror.ErrorID(status.LastMountErr) || ps.ctx.V(2) {
			ps.ctx.Errorf("rpc pub: couldn't mount(%v, %v, %v): %v", name, server, ttl, status.LastMountErr)
		}
	} else {
		if last.LastMount.IsZero() || last.LastMountErr != nil || ps.ctx.V(2) {
			ps.ctx.Infof("rpc pub: mount(%v, %v, %v)", name, server, ttl)
		}
	}
}

func (ps *pubState) sync() {
	ps.deadline = time.Now().Add(ps.period) // set deadline for the next sync
	for key, status := range ps.mounts {
		if status.LastUnmountErr != nil {
			// Desired state is "unmounted", failed at previous attempt. Retry.
			ps.unmount(key.name, key.server, status, true)
		} else {
			ps.mount(key.name, key.server, status, ps.names[key.name])
		}
	}
}

func (ps *pubState) unmount(name, server string, status *rpc.MountStatus, retry bool) {
	status.LastUnmount = time.Now()
	var opts []naming.NamespaceOpt
	if !retry {
		opts = []naming.NamespaceOpt{options.NoRetry{}}
	}
	status.LastUnmountErr = ps.ns.Unmount(ps.ctx, name, server, opts...)
	if status.LastUnmountErr != nil {
		ps.ctx.Errorf("rpc pub: couldn't unmount(%v, %v): %v", name, server, status.LastUnmountErr)
	} else {
		ps.ctx.VI(1).Infof("rpc pub: unmount(%v, %v)", name, server)
		delete(ps.mounts, mountKey{name, server})
	}
}

func (ps *pubState) unmountAll() {
	for key, status := range ps.mounts {
		ps.unmount(key.name, key.server, status, false)
	}
}

func copyNamesToSlice(sl map[string]nameAttr) []string {
	var ret []string
	for s, _ := range sl {
		if len(s) == 0 {
			continue
		}
		ret = append(ret, s)
	}
	return ret
}

func copyServersToSlice(sl map[string]bool) []string {
	var ret []string
	for s, _ := range sl {
		if len(s) == 0 {
			continue
		}
		ret = append(ret, s)
	}
	return ret
}

func (ps *pubState) getStatus() rpc.MountState {
	st := make([]rpc.MountStatus, 0, len(ps.mounts))
	names := copyNamesToSlice(ps.names)
	servers := copyServersToSlice(ps.servers)
	sort.Strings(names)
	sort.Strings(servers)
	for _, name := range names {
		for _, server := range servers {
			if v := ps.mounts[mountKey{name, server}]; v != nil {
				mst := *v
				mst.Name = name
				mst.Server = server
				st = append(st, mst)
			}
		}
	}
	return st
}

// TODO(toddw): sort the names/servers so that the output order is stable.
func (ps *pubState) debugString() string {
	l := make([]string, 2+len(ps.mounts))
	l = append(l, fmt.Sprintf("Publisher period:%v deadline:%v", ps.period, ps.deadline))
	l = append(l, "==============================Mounts============================================")
	for key, status := range ps.mounts {
		l = append(l, fmt.Sprintf("[%s,%s] mount(%v, %v, %s) unmount(%v, %v)", key.name, key.server, status.LastMount, status.LastMountErr, status.TTL, status.LastUnmount, status.LastUnmountErr))
	}
	return strings.Join(l, "\n")
}

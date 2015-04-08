// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package namespace

import (
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/namespace"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/verror"
)

type Server struct {
	ns namespace.T
}

func New(ctx *context.T) *Server {
	return &Server{v23.GetNamespace(ctx)}
}

func (s *Server) Glob(call *NamespaceGlobServerCallStub, pattern string) error {
	// Call Glob on the namespace client instance
	ch, err := s.ns.Glob(call.Context(), pattern)
	if err != nil {
		return err
	}

	stream := call.SendStream()

	for mp := range ch {
		var reply naming.GlobReply
		switch v := mp.(type) {
		case *naming.GlobError:
			reply = naming.GlobReplyError{*v}
		case *naming.MountEntry:
			reply = naming.GlobReplyEntry{*v}
		}
		if err = stream.Send(reply); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Mount(call rpc.ServerCall, name, server string, ttl time.Duration, replace bool) error {
	rmOpt := naming.ReplaceMount(replace)
	err := s.ns.Mount(call.Context(), name, server, ttl, rmOpt)
	if err != nil {
		err = verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return err
}

func (s *Server) Unmount(call rpc.ServerCall, name, server string) error {
	return s.ns.Unmount(call.Context(), name, server)
}

func (s *Server) Resolve(call rpc.ServerCall, name string) ([]string, error) {
	me, err := s.ns.Resolve(call.Context(), name)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return me.Names(), nil
}

func (s *Server) ResolveToMountTable(call rpc.ServerCall, name string) ([]string, error) {
	me, err := s.ns.ResolveToMountTable(call.Context(), name)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return me.Names(), nil
}

func (s *Server) FlushCacheEntry(call rpc.ServerCall, name string) (bool, error) {
	return s.ns.FlushCacheEntry(name), nil
}

func (s *Server) DisableCache(call rpc.ServerCall, disable bool) error {
	disableCacheCtl := naming.DisableCache(disable)
	_ = s.ns.CacheCtl(disableCacheCtl)
	return nil
}

func (s *Server) Roots(call rpc.ServerCall) ([]string, error) {
	return s.ns.Roots(), nil
}

func (s *Server) SetRoots(call rpc.ServerCall, roots []string) error {
	if err := s.ns.SetRoots(roots...); err != nil {
		return verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return nil
}

func (s *Server) SetPermissions(call rpc.ServerCall, name string, acl access.Permissions, version string) error {
	return s.ns.SetPermissions(call.Context(), name, acl, version)
}

func (s *Server) GetPermissions(call rpc.ServerCall, name string) (access.Permissions, string, error) {
	return s.ns.GetPermissions(call.Context(), name)
}

func (s *Server) Delete(call rpc.ServerCall, name string, deleteSubtree bool) error {
	return s.ns.Delete(call.Context(), name, deleteSubtree)
}

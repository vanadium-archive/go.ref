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

func (s *Server) Glob(ctx *context.T, call *NamespaceGlobServerCallStub, pattern string) error {
	// Call Glob on the namespace client instance
	ch, err := s.ns.Glob(ctx, pattern)
	if err != nil {
		return err
	}

	stream := call.SendStream()

	for mp := range ch {
		if err = stream.Send(mp); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Mount(ctx *context.T, _ rpc.ServerCall, name, server string, ttl time.Duration, replace bool) error {
	rmOpt := naming.ReplaceMount(replace)
	err := s.ns.Mount(ctx, name, server, ttl, rmOpt)
	if err != nil {
		err = verror.Convert(verror.ErrInternal, ctx, err)
	}
	return err
}

func (s *Server) Unmount(ctx *context.T, _ rpc.ServerCall, name, server string) error {
	return s.ns.Unmount(ctx, name, server)
}

func (s *Server) Resolve(ctx *context.T, _ rpc.ServerCall, name string) ([]string, error) {
	me, err := s.ns.Resolve(ctx, name)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, ctx, err)
	}
	return me.Names(), nil
}

func (s *Server) ResolveToMountTable(ctx *context.T, _ rpc.ServerCall, name string) ([]string, error) {
	me, err := s.ns.ResolveToMountTable(ctx, name)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, ctx, err)
	}
	return me.Names(), nil
}

func (s *Server) FlushCacheEntry(ctx *context.T, _ rpc.ServerCall, name string) (bool, error) {
	return s.ns.FlushCacheEntry(ctx, name), nil
}

func (s *Server) DisableCache(_ *context.T, _ rpc.ServerCall, disable bool) error {
	disableCacheCtl := naming.DisableCache(disable)
	_ = s.ns.CacheCtl(disableCacheCtl)
	return nil
}

func (s *Server) Roots(*context.T, rpc.ServerCall) ([]string, error) {
	return s.ns.Roots(), nil
}

func (s *Server) SetRoots(ctx *context.T, _ rpc.ServerCall, roots []string) error {
	if err := s.ns.SetRoots(roots...); err != nil {
		return verror.Convert(verror.ErrInternal, ctx, err)
	}
	return nil
}

func (s *Server) SetPermissions(ctx *context.T, _ rpc.ServerCall, name string, perms access.Permissions, version string) error {
	return s.ns.SetPermissions(ctx, name, perms, version)
}

func (s *Server) GetPermissions(ctx *context.T, _ rpc.ServerCall, name string) (access.Permissions, string, error) {
	return s.ns.GetPermissions(ctx, name)
}

func (s *Server) Delete(ctx *context.T, _ rpc.ServerCall, name string, deleteSubtree bool) error {
	return s.ns.Delete(ctx, name, deleteSubtree)
}

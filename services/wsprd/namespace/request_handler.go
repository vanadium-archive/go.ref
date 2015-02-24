package namespace

import (
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/verror"
)

type Server struct {
	ns naming.Namespace
}

func New(ctx *context.T) *Server {
	return &Server{v23.GetNamespace(ctx)}
}

func (s *Server) Glob(ctx *NamespaceGlobContextStub, pattern string) error {
	// Call Glob on the namespace client instance
	ch, err := s.ns.Glob(ctx.Context(), pattern)
	if err != nil {
		return err
	}

	stream := ctx.SendStream()

	for mp := range ch {
		var reply naming.VDLGlobReply
		switch v := mp.(type) {
		case *naming.GlobError:
			reply = naming.VDLGlobReplyError{*v}
		case *naming.MountEntry:
			reply = naming.VDLGlobReplyEntry{convertToVDLEntry(*v)}
		}
		if err = stream.Send(reply); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Mount(ctx ipc.ServerContext, name, server string, ttl time.Duration, replace bool) error {
	rmOpt := naming.ReplaceMountOpt(replace)
	err := s.ns.Mount(ctx.Context(), name, server, ttl, rmOpt)
	if err != nil {
		err = verror.Convert(verror.ErrInternal, ctx.Context(), err)
	}
	return err
}

func (s *Server) Unmount(ctx ipc.ServerContext, name, server string) error {
	return s.ns.Unmount(ctx.Context(), name, server)
}

func (s *Server) Resolve(ctx ipc.ServerContext, name string) ([]string, error) {
	me, err := s.ns.Resolve(ctx.Context(), name)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, ctx.Context(), err)
	}
	return me.Names(), nil
}

func (s *Server) ResolveToMT(ctx ipc.ServerContext, name string) ([]string, error) {
	me, err := s.ns.ResolveToMountTable(ctx.Context(), name)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, ctx.Context(), err)
	}
	return me.Names(), nil
}

func (s *Server) FlushCacheEntry(ctx ipc.ServerContext, name string) (bool, error) {
	return s.ns.FlushCacheEntry(name), nil
}

func (s *Server) DisableCache(ctx ipc.ServerContext, disable bool) error {
	disableCacheCtl := naming.DisableCache(disable)
	_ = s.ns.CacheCtl(disableCacheCtl)
	return nil
}

func (s *Server) Roots(ctx ipc.ServerContext) ([]string, error) {
	return s.ns.Roots(), nil
}

func (s *Server) SetRoots(ctx ipc.ServerContext, roots []string) error {
	if err := s.ns.SetRoots(roots...); err != nil {
		return verror.Convert(verror.ErrInternal, ctx.Context(), err)
	}
	return nil
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

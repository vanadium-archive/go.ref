package namespace

import (
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
	"v.io/v23/naming/ns"
	"v.io/v23/services/security/access"
	"v.io/v23/verror"
)

type Server struct {
	ns ns.Namespace
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

func (s *Server) Mount(call ipc.ServerCall, name, server string, ttl time.Duration, replace bool) error {
	rmOpt := naming.ReplaceMountOpt(replace)
	err := s.ns.Mount(call.Context(), name, server, ttl, rmOpt)
	if err != nil {
		err = verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return err
}

func (s *Server) Unmount(call ipc.ServerCall, name, server string) error {
	return s.ns.Unmount(call.Context(), name, server)
}

func (s *Server) Resolve(call ipc.ServerCall, name string) ([]string, error) {
	me, err := s.ns.Resolve(call.Context(), name)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return me.Names(), nil
}

func (s *Server) ResolveToMT(call ipc.ServerCall, name string) ([]string, error) {
	me, err := s.ns.ResolveToMountTable(call.Context(), name)
	if err != nil {
		return nil, verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return me.Names(), nil
}

func (s *Server) FlushCacheEntry(call ipc.ServerCall, name string) (bool, error) {
	return s.ns.FlushCacheEntry(name), nil
}

func (s *Server) DisableCache(call ipc.ServerCall, disable bool) error {
	disableCacheCtl := naming.DisableCache(disable)
	_ = s.ns.CacheCtl(disableCacheCtl)
	return nil
}

func (s *Server) Roots(call ipc.ServerCall) ([]string, error) {
	return s.ns.Roots(), nil
}

func (s *Server) SetRoots(call ipc.ServerCall, roots []string) error {
	if err := s.ns.SetRoots(roots...); err != nil {
		return verror.Convert(verror.ErrInternal, call.Context(), err)
	}
	return nil
}

func (s *Server) SetACL(call ipc.ServerCall, name string, acl access.TaggedACLMap, etag string) error {
	return s.ns.SetACL(call.Context(), name, acl, etag)
}

func (s *Server) GetACL(call ipc.ServerCall, name string) (access.TaggedACLMap, string, error) {
	return s.ns.GetACL(call.Context(), name)
}

func (s *Server) Delete(call ipc.ServerCall, name string, deleteSubtree bool) error {
	return s.ns.Delete(call.Context(), name, deleteSubtree)
}

func convertToVDLEntry(value naming.MountEntry) naming.VDLMountEntry {
	result := naming.VDLMountEntry{
		Name: value.Name,
		MT:   value.ServesMountTable(),
	}
	for _, s := range value.Servers {
		result.Servers = append(result.Servers,
			naming.VDLMountedServer{
				Server:           s.Server,
				TTL:              uint32(s.Expires.Sub(time.Now())),
				BlessingPatterns: s.BlessingPatterns,
			})
	}
	return result
}

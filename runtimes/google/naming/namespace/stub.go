package namespace

import (
	"time"

	"veyron.io/veyron/veyron2/naming"
)

func convertServersToStrings(servers []naming.MountedServer, suffix string) (ret []string) {
	for _, s := range servers {
		ret = append(ret, naming.Join(s.Server, suffix))
	}
	return
}

func convertStringsToServers(servers []string) (ret []naming.MountedServer) {
	for _, s := range servers {
		ret = append(ret, naming.MountedServer{Server: s})
	}
	return
}

func convertServers(servers []naming.VDLMountedServer) []naming.MountedServer {
	var reply []naming.MountedServer
	for _, s := range servers {
		if s.TTL == 0 {
			s.TTL = 32000000 // > 1 year
		}
		expires := time.Now().Add(time.Duration(s.TTL) * time.Second)
		reply = append(reply, naming.MountedServer{Server: s.Server, Expires: expires})
	}
	return reply
}

func convertMountEntry(e *naming.VDLMountEntry) *naming.MountEntry {
	v := &naming.MountEntry{Name: e.Name, Servers: convertServers(e.Servers)}
	v.SetServesMountTable(e.MT)
	return v
}

package namespace

import "v.io/v23/naming"

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
